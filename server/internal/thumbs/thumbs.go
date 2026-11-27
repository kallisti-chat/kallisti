// Package thumbs makes small pictures of uploads with ffmpeg, if it is
// installed: a frame of a video, a smaller copy of a large picture.
// ffmpeg runs as a separate process, at low priority (nice), with a time
// limit, and on the stored file only: local files as the only protocol,
// the demuxer given by the upload's type (no playlists that point
// elsewhere). It writes to the temporary directory; the result is then
// copied next to the upload and renamed into place, so no half-written
// picture is ever served.
package thumbs

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"kallisti.chat/server/internal/imagesize"
)

// Format is a picture format ffmpeg writes.
type Format struct {
	Type  string // MIME type
	Codec string
	Muxer string
	Alpha bool     // keeps transparency
	Args  []string // encoder options
}

var (
	WebP = Format{Type: "image/webp", Codec: "libwebp", Muxer: "webp", Alpha: true}
	JPEG = Format{Type: "image/jpeg", Codec: "mjpeg", Muxer: "mjpeg", Args: []string{"-pix_fmt", "yuvj420p", "-q:v", "4"}}
	// A GIF's video: H.264 (every browser plays it), playable while it
	// loads; about a tenth of the GIF.
	MP4 = Format{Type: "video/mp4", Codec: "libx264", Muxer: "mp4", Args: []string{"-pix_fmt", "yuv420p", "-crf", "26", "-preset", "slow", "-movflags", "+faststart"}}
)

// demuxers by upload type: ffmpeg reads nothing else.
var demuxers = map[string]string{
	"video/mp4":  "mov",
	"video/webm": "matroska",
	"image/png":  "png_pipe",
	"image/jpeg": "jpeg_pipe",
	"image/webp": "webp_pipe",
	"image/jxl":  "jpegxl_pipe", // also the container; libjxl turns by the orientation
	"image/gif":  "gif",         // as a video only (see Job.Video)
}

// Supported: Make can read uploads of this type.
func Supported(ctype string) bool { _, ok := demuxers[ctype]; return ok }

// Job is one picture to make.
type Job struct {
	Src, Dst string // the stored upload; where the picture goes
	Type     string // the upload's type (see Supported)
	Max      int    // the longer side at most, in pixels
	Seek     bool   // a video's frame at 1 s, not the first
	Video    bool   // all frames, as a video (MP4), not one picture
	// Frames with more pixels are not decoded (0: imagesize.MaxPixels).
	// ffmpeg checks the coded frames, whatever the container says.
	MaxPixels int
	// A JPEG's Exif orientation: ffmpeg is told to ignore it and the
	// picture is turned here, as browsers do.
	Orientation uint16
}

// exifFilters turn a picture by its Exif orientation.
var exifFilters = map[uint16]string{2: "hflip", 3: "hflip,vflip", 4: "vflip", 5: "transpose=0", 6: "transpose=1", 7: "transpose=3", 8: "transpose=2"}

// Args are ffmpeg's arguments for a job, writing to out (a video: MP4,
// whatever f).
func Args(f Format, j Job, out string) []string {
	if j.Video {
		f = MP4
	}
	// One thread each for decoding and encoding: less memory and CPU.
	a := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file", "-threads", "1"}
	if j.Seek {
		a = append(a, "-ss", "1")
	}
	if strings.HasPrefix(j.Type, "image/") {
		a = append(a, "-noautorotate")
	}
	a = append(a, "-max_pixels", strconv.Itoa(cmp.Or(j.MaxPixels, imagesize.MaxPixels)))
	side := strconv.Itoa(j.Max)
	vf := "scale=w='min(" + side + ",iw)':h='min(" + side + ",ih)':force_original_aspect_ratio=decrease"
	if t, ok := exifFilters[j.Orientation]; ok {
		vf = t + "," + vf
	}
	frames := []string{"-frames:v", "1"}
	if j.Video {
		vf += ",scale=trunc(iw/2)*2:trunc(ih/2)*2" // H.264 wants even sides
		frames = nil
	}
	a = append(a, "-f", demuxers[j.Type], "-i", "file:"+j.Src)
	a = append(a, frames...)
	a = append(a, "-an", "-sn", "-dn", "-vf", vf, "-c:v", f.Codec, "-threads", "1")
	if j.Video {
		// GIF delays are hundredths of a second: each frame keeps its own
		// (not a guessed frame rate, which drops and moves frames). And
		// no file over the limit, not even while encoding.
		a = append(a, "-enc_time_base", "1/100", "-fs", strconv.Itoa(maxOutput))
	}
	if f.Args != nil {
		a = append(a, f.Args...)
	} else if f.Alpha && j.Type != "image/jpeg" && strings.HasPrefix(j.Type, "image/") {
		a = append(a, "-pix_fmt", "yuva420p")
	} else {
		a = append(a, "-pix_fmt", "yuv420p")
	}
	return append(a, "-f", f.Muxer, "-y", "file:"+out)
}

// Tool is an ffmpeg to run and the format it writes.
type Tool struct {
	Command []string // ffmpeg, behind nice where there is one
	Format  Format
	Video   bool // it writes MP4 (H.264) too
}

// Find looks for ffmpeg (path, or on PATH) and asks which encoders it has:
// WebP if it can, else JPEG.
func Find(path string) (Tool, error) {
	return find(path, exec.LookPath, func(cmd []string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, cmd[0], cmd[1:]...).Output() // #nosec G204 -- FFMPEG_PATH (the operator's) and fixed flags
	})
}

func find(path string, look func(string) (string, error), output func([]string) ([]byte, error)) (Tool, error) {
	ffmpeg, err := look(cmp.Or(path, "ffmpeg"))
	if err != nil {
		return Tool{}, err
	}
	out, err := output([]string{ffmpeg, "-hide_banner", "-encoders"})
	if err != nil {
		return Tool{}, fmt.Errorf("%s -encoders: %w", ffmpeg, err)
	}
	f, ok := formatOf(string(out))
	if !ok {
		return Tool{}, fmt.Errorf("%s has no WebP or JPEG encoder", ffmpeg)
	}
	cmd := []string{ffmpeg}
	if nice, err := look("nice"); err == nil {
		cmd = []string{nice, "-n", "10", ffmpeg}
	}
	return Tool{Command: cmd, Format: f, Video: strings.Contains(string(out), " "+MP4.Codec+" ")}, nil
}

// formatOf picks the format from `ffmpeg -encoders` (lines of flags, name,
// description).
func formatOf(encoders string) (Format, bool) {
	has := map[string]bool{}
	for line := range strings.Lines(encoders) {
		if f := strings.Fields(line); len(f) >= 2 {
			has[f[1]] = true
		}
	}
	switch {
	case has[WebP.Codec]:
		return WebP, true
	case has[JPEG.Codec]:
		return JPEG, true
	}
	return Format{}, false
}

const (
	// A picture larger than this is not kept (they are some 100 KB).
	maxOutput = 8 << 20
	// Text of ffmpeg's errors kept for the log.
	maxStderr = 1024
)

// Maker runs jobs: at most a few ffmpeg processes at once, from a bounded
// queue (Enqueue) or directly (Make).
type Maker struct {
	tool    Tool
	timeout time.Duration // per job
	run     func(ctx context.Context, args []string) error
	slots   chan struct{}
	queue   chan task
}

type task struct {
	job  Job
	done func(ctype string, err error)
}

// New starts workers that take jobs from a queue of queued at most, until
// ctx ends (jobs stopped by that are not reported).
func New(ctx context.Context, tool Tool, workers, queued int) *Maker {
	m := &Maker{tool: tool, timeout: 20 * time.Second, slots: make(chan struct{}, workers), queue: make(chan task, queued)}
	m.run = m.exec
	for range workers {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-m.queue:
					if ctype, err := m.Make(ctx, t.job); ctx.Err() == nil {
						t.done(ctype, err)
					}
				}
			}
		}()
	}
	return m
}

// Format is what the pictures are.
func (m *Maker) Format() Format { return m.tool.Format }

// Video: it makes videos (see Job.Video).
func (m *Maker) Video() bool { return m.tool.Video }

// Enqueue queues a job; done gets the picture's type, or an error. False:
// the queue is full (the job is dropped).
func (m *Maker) Enqueue(j Job, done func(ctype string, err error)) bool {
	select {
	case m.queue <- task{j, done}:
		return true
	default:
		return false
	}
}

// ErrFailed: ffmpeg failed on the file, took too long, or wrote nothing
// usable; trying again will not help. Other errors (files, shutdown) may
// pass.
var ErrFailed = errors.New("no preview from this file")

// errPassing: ffmpeg did not start, or the disk was full (not ErrFailed).
var errPassing = errors.New("not the file's fault")

// Make makes a picture now (waiting for a free slot) and returns its type.
func (m *Maker) Make(ctx context.Context, j Job) (string, error) {
	if !Supported(j.Type) {
		return "", fmt.Errorf("%w: cannot read %s", ErrFailed, j.Type)
	}
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	tmp, err := os.CreateTemp("", "kallisti-preview-*")
	if err != nil {
		return "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	timeout, ctype := m.timeout, m.tool.Format.Type
	if j.Video { // all frames: longer
		timeout, ctype = 3*m.timeout, MP4.Type
	}
	limit, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = m.run(limit, Args(m.tool.Format, j, tmp.Name()))
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case limit.Err() != nil:
		return "", fmt.Errorf("%w: ffmpeg: %w", ErrFailed, limit.Err())
	case errors.Is(err, errPassing):
		return "", err
	case err != nil:
		return "", fmt.Errorf("%w: %w", ErrFailed, err)
	}
	if err := place(tmp.Name(), j.Dst); err != nil {
		return "", err
	}
	return ctype, nil
}

// exec runs ffmpeg with no environment but PATH (none of the server's
// secrets); it is killed when ctx ends.
func (m *Maker) exec(ctx context.Context, args []string) error { return m.execIO(ctx, args, nil, nil) }

// execIO is exec with stdin (nil: none) and stdout (nil: discarded).
func (m *Maker) execIO(ctx context.Context, args []string, stdin []byte, stdout io.Writer) error {
	// #nosec G204 -- the command is Find's; args are Args or StillArgs: fixed
	// flags, numbers and file: paths of random keys, no shell.
	cmd := exec.CommandContext(ctx, m.tool.Command[0], slices.Concat(m.tool.Command[1:], args)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout = stdout
	cmd.WaitDelay = time.Second
	var stderr limited
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: ffmpeg: %w", errPassing, err)
	}
	if err := cmd.Wait(); err != nil {
		err = fmt.Errorf("ffmpeg: %w: %s", err, bytes.TrimSpace(stderr.b))
		if stderr.diskFull {
			err = fmt.Errorf("%w: %w", errPassing, err)
		}
		return err
	}
	return nil
}

// place copies the picture next to dst and renames it into place (the
// temporary directory can be another file system).
func place(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- Make's os.CreateTemp file
	if err != nil {
		return err
	}
	defer in.Close()
	if st, err := in.Stat(); err != nil || st.Size() == 0 || st.Size() > maxOutput {
		return cmp.Or(err, fmt.Errorf("%w: ffmpeg wrote no picture, or a huge one", ErrFailed))
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".preview-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(in, maxOutput))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(out.Name(), dst)
	}
	if err != nil {
		os.Remove(out.Name())
	}
	return err
}

// limited keeps the first maxStderr bytes written, and sees all of it for
// a full disk (ffmpeg writes a line at a time).
type limited struct {
	b        []byte
	diskFull bool
}

var diskFull = regexp.MustCompile(`(?i)no space left on device|quota exceeded`)

func (l *limited) Write(p []byte) (int, error) {
	l.b = append(l.b, p[:min(len(p), maxStderr-len(l.b))]...)
	l.diskFull = l.diskFull || diskFull.Match(p)
	return len(p), nil
}
