// Package recording records voice channels: one Ogg Opus file per speaker
// from the packets as they come (no encoding again), aligned in time. The
// finished recordings wait in a directory for staff to download them (a
// zip), for a limited time.
package recording

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// queueSize: packets waiting for the recorder (about a second of 50
// speakers). When it is full, packets are dropped: voice never waits.
const queueSize = 1024

// maxTracks: speakers recorded, at most (each track is an open file until
// the end); later ones are not recorded, and named in the info (as many).
const maxTracks = 50

// Limits of one recording: it stops itself at either.
type Limits struct {
	MaxDuration time.Duration
	MaxBytes    int64
}

// Packet is a voice packet of a speaker in the recorded channel.
type Packet struct {
	Speaker    string // the track: the user's name
	Seq        uint64 // Mumble's sequence number (10 ms frames)
	Opus       []byte
	Terminator bool
	At         time.Time // when it came
}

// Info describes a recording (recording.json next to the tracks).
type Info struct {
	ID          string      `json:"id"`
	Channel     uint32      `json:"channel"`
	ChannelName string      `json:"channelName"`
	By          string      `json:"by"`
	Started     time.Time   `json:"started"`
	Stopped     time.Time   `json:"stopped"`
	Expires     time.Time   `json:"expires"`
	Reason      string      `json:"reason"` // why it stopped
	Dropped     uint64      `json:"dropped,omitempty"`
	Size        int64       `json:"size"` // bytes of all tracks
	Tracks      []TrackInfo `json:"tracks"`
	Unrecorded  []string    `json:"unrecorded,omitempty"` // speakers beyond maxTracks
}

// TrackInfo: a speaker's file. Every track starts at the recording's
// start; Start is where the speaker first spoke (ms).
type TrackInfo struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Channels int    `json:"channels"`
	Start    int64  `json:"start"`
}

const infoFile = "recording.json"

// Library keeps the recordings in Dir: one directory each.
type Library struct {
	Dir    string
	Limits Limits
	Keep   time.Duration // finished recordings are removed after this
	Now    func() time.Time

	mu     sync.Mutex
	active map[string]bool
}

// Recording is a running recording. Feed it packets; Stop ends it.
type Recording struct {
	lib     *Library
	info    Info
	in      chan Packet
	quit    chan struct{}
	done    chan struct{}
	dropped atomic.Uint64
	once    sync.Once
	reason  string // set before quit closes
	// Tracks by speaker, in the order they first spoke (the recorder's).
	tracks map[string]*track
	order  []*track
	files  map[string]bool // lower-case file names taken
}

// Start begins recording a channel. onLimit hears it stop by itself (at a
// limit), after its files are closed.
func (l *Library) Start(channel uint32, channelName, by string, onLimit func(Info)) (*Recording, error) {
	id := make([]byte, 8)
	rand.Read(id)
	r := &Recording{
		lib:    l,
		info:   Info{ID: hex.EncodeToString(id), Channel: channel, ChannelName: channelName, By: by, Started: l.Now(), Tracks: []TrackInfo{}},
		in:     make(chan Packet, queueSize),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
		tracks: map[string]*track{},
		files:  map[string]bool{strings.ToLower(infoFile): true},
	}
	if err := os.MkdirAll(r.dir(), 0o700); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.active == nil {
		l.active = map[string]bool{}
	}
	l.active[r.info.ID] = true
	l.mu.Unlock()
	go r.run(onLimit)
	return r, nil
}

func (r *Recording) ID() string  { return r.info.ID }
func (r *Recording) dir() string { return filepath.Join(r.lib.Dir, r.info.ID) }

// Feed hands a packet to the recorder, or drops it if the queue is full.
// It never blocks; after the end packets are lost.
func (r *Recording) Feed(p Packet) {
	if len(r.in) == cap(r.in) {
		r.dropped.Add(1)
		return
	}
	p.Opus = append([]byte(nil), p.Opus...) // the caller's buffer is reused
	select {
	case r.in <- p:
	default:
		r.dropped.Add(1)
	}
}

// Ended: the recording stopped and its files are closed.
func (r *Recording) Ended() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Stop ends the recording (if it still runs) and waits until its files
// are closed. It returns the recording's info.
func (r *Recording) Stop(reason string) Info {
	r.once.Do(func() {
		r.reason = reason
		close(r.quit)
	})
	<-r.done
	return r.info
}

func (r *Recording) run(onLimit func(Info)) {
	timer := time.NewTimer(r.lib.Limits.MaxDuration)
	defer timer.Stop()
	reason := ""
	for reason == "" {
		select {
		case p := <-r.in:
			reason = r.add(p)
		case <-timer.C:
			reason = "time limit"
		case <-r.quit:
			// What came before the stop is still recorded.
			for len(r.in) > 0 && r.add(<-r.in) == "" {
			}
			reason = r.reason
		}
	}
	limited := false
	r.once.Do(func() { limited = true }) // Stop no longer closes quit
	r.finish(reason)
	close(r.done)
	if limited && onLimit != nil {
		onLimit(r.info)
	}
}

// add records a packet; it returns a reason to stop ("": none).
func (r *Recording) add(p Packet) string {
	t := r.tracks[p.Speaker]
	if _, _, ok := packetSamples(p.Opus); !ok {
		return ""
	}
	if t == nil && len(r.order) >= maxTracks {
		if len(r.info.Unrecorded) < maxTracks && !slices.Contains(r.info.Unrecorded, p.Speaker) {
			slog.Warn("recording: too many speakers, not recorded", "id", r.info.ID, "speaker", p.Speaker)
			r.info.Unrecorded = append(r.info.Unrecorded, p.Speaker)
		}
		return ""
	}
	if t == nil {
		var err error
		if t, err = createTrack(filepath.Join(r.dir(), r.fileFor(p.Speaker)), p.Speaker, uint32(len(r.order)+1)); err != nil {
			slog.Error("recording: new track", "err", err)
			return "error"
		}
		r.tracks[p.Speaker], r.order = t, append(r.order, t)
	}
	at := max(0, p.At.Sub(r.info.Started).Microseconds()*sampleRate/1e6)
	if err := t.add(p.Opus, p.Seq, p.Terminator, at, r.lib.Limits.MaxBytes-r.size()); errors.Is(err, errSizeLimit) {
		return "size limit"
	} else if err != nil {
		slog.Error("recording: writing a track", "track", t.name, "err", err)
		return "write error"
	}
	if r.size() > r.lib.Limits.MaxBytes {
		return "size limit"
	}
	return ""
}

func (r *Recording) size() int64 {
	var n int64
	for _, t := range r.order {
		n += t.bytes()
	}
	return n
}

// fileFor: a file name for a speaker (unique, also on case-insensitive
// file systems).
func (r *Recording) fileFor(speaker string) string {
	base := safeName(speaker)
	name := base + ".opus"
	for i := 2; r.files[strings.ToLower(name)]; i++ {
		name = base + "-" + strconv.Itoa(i) + ".opus"
	}
	r.files[strings.ToLower(name)] = true
	return name
}

// safeName keeps a name's letters, digits and a few signs, for file names.
func safeName(s string) string {
	s = strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || strings.ContainsRune(" -_.", c) {
			return c
		}
		return '_'
	}, s)
	s = strings.Trim(s, " .")
	if r := []rune(s); len(r) > 64 {
		s = string(r[:64])
	}
	if s == "" {
		return "speaker"
	}
	return s
}

// padBytes: a second of silence on a track and its last page, at most
// (see silenceBytes).
const padBytes = 512

// finish closes the tracks and writes the info. Silence fills them up to
// the end a second at a time, within the size limit: at it they end
// earlier (where each is, if it was reached already).
func (r *Recording) finish(reason string) {
	r.info.Stopped = r.lib.Now()
	r.info.Expires = r.info.Stopped.Add(r.lib.Keep)
	r.info.Reason, r.info.Dropped = reason, r.dropped.Load()
	end := max(0, r.info.Stopped.Sub(r.info.Started).Microseconds()*sampleRate/1e6)
	reached := end
	for _, t := range r.order {
		reached = min(reached, t.pos)
	}
	for reached < end {
		if r.size()+int64(len(r.order))*padBytes > r.lib.Limits.MaxBytes {
			if reason != "write error" {
				r.info.Reason = "size limit"
			}
			break
		}
		reached = min(end, reached+sampleRate)
		for _, t := range r.order {
			t.fill(reached)
		}
	}
	for _, t := range r.order {
		if err := t.finish(reached); err != nil {
			slog.Error("recording: closing a track", "track", t.name, "err", err)
			r.info.Reason = "write error"
		}
		channels := 1
		if t.stereo {
			channels = 2
		}
		r.info.Tracks = append(r.info.Tracks, TrackInfo{Name: t.name, File: filepath.Base(t.f.Name()), Channels: channels, Start: max(0, t.first) / (sampleRate / 1000)})
		r.info.Size += t.bytes()
	}
	if err := writeInfo(r.dir(), r.info); err != nil {
		slog.Error("recording: info", "id", r.info.ID, "err", err)
	}
	r.lib.mu.Lock()
	delete(r.lib.active, r.info.ID)
	r.lib.mu.Unlock()
}

// writeInfo writes recording.json whole (a reader never sees half).
func writeInfo(dir string, info Info) error {
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, infoFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, infoFile))
}

// validID: what Start makes (16 hex digits); nothing else names a
// directory.
func validID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 16 && err == nil && strings.ToLower(id) == id
}

func (l *Library) info(id string) (Info, error) {
	if !validID(id) {
		return Info{}, os.ErrNotExist
	}
	var info Info
	b, err := os.ReadFile(filepath.Join(l.Dir, id, infoFile)) // #nosec G304 G703 -- a validID (checked above)
	if err == nil {
		err = json.Unmarshal(b, &info)
	}
	return info, err
}

// finished: a recording that may still be downloaded.
func (l *Library) finished(id string) (Info, bool) {
	info, err := l.info(id)
	return info, err == nil && !l.Now().After(info.Expires)
}

// List: the finished recordings, newest first.
func (l *Library) List() []Info {
	entries, _ := os.ReadDir(l.Dir)
	out := []Info{}
	for _, e := range entries {
		if info, ok := l.finished(e.Name()); ok {
			out = append(out, info)
		}
	}
	slices.SortFunc(out, func(a, b Info) int { return b.Started.Compare(a.Started) })
	return out
}

// Clean removes recordings whose download period is over, and leftovers
// of recordings a crash cut off (once they cannot run any more).
func (l *Library) Clean() {
	now := l.Now()
	entries, _ := os.ReadDir(l.Dir)
	for _, e := range entries {
		id := e.Name()
		l.mu.Lock()
		active := l.active[id]
		l.mu.Unlock()
		if !validID(id) || active {
			continue
		}
		expired := false
		if info, err := l.info(id); err == nil {
			expired = now.After(info.Expires)
		} else if errors.Is(err, os.ErrNotExist) {
			fi, err := e.Info()
			expired = err == nil && now.After(fi.ModTime().Add(l.Keep+l.Limits.MaxDuration))
		}
		if expired {
			if err := os.RemoveAll(filepath.Join(l.Dir, id)); err != nil {
				slog.Error("recording: removing", "id", id, "err", err)
			}
		}
	}
}
