package whip

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/stream"
)

// clock turns a track's RTP timestamps into stream microseconds, counted
// from the arrival of its first packet (arrival: microseconds since the
// session began). Picture and sound each start at their first arrival, so
// they are in step up to the difference in network delay.
type clock struct {
	rate    int64
	started bool
	last    uint32
	ticks   int64
	base    int64
}

func (c *clock) micros(ts uint32, arrival int64) uint64 {
	if !c.started {
		c.started, c.last, c.base = true, ts, arrival
	}
	c.ticks += int64(int32(ts - c.last)) // wraps around
	c.last = ts
	return uint64(max(0, c.base+c.ticks*1_000_000/c.rate))
}

// videoIn builds access units from RTP packets (reordered, H.264
// depacketized to Annex B).
type videoIn struct {
	sb    *samplebuilder.SampleBuilder
	clock clock
	avc   avc
}

const (
	maxLatePackets = 1024 // a 1 MB keyframe (the hub's limit) in 1200-byte packets fits
	maxLate        = 500 * time.Millisecond
)

func newVideoIn() *videoIn {
	return &videoIn{
		sb:    samplebuilder.New(maxLatePackets, &h264Depacketizer{}, 90_000, samplebuilder.WithMaxTimeDelay(maxLate)),
		clock: clock{rate: 90_000},
	}
}

const maxUnit = 1 << 20 // bytes of a picture, and of a NAL unit in fragments (the hub's frame limit)

// h264Depacketizer bounds Pion's FU-A assembly, which appends fragments
// until an end one: a start fragment drops an unfinished unit, and one past
// maxUnit is dropped (a picture of only it comes out empty: lost, so a
// keyframe is asked for).
type h264Depacketizer struct {
	codecs.H264Packet
	frag int  // bytes of the unit in fragments so far
	open bool // its start came
}

func (d *h264Depacketizer) Unmarshal(payload []byte) ([]byte, error) {
	if len(payload) < 2 || payload[0]&0x1f != 28 { // not FU-A
		d.open = false
		return d.H264Packet.Unmarshal(payload)
	}
	if payload[1]&0x80 != 0 {
		d.H264Packet, d.frag, d.open = codecs.H264Packet{}, 0, true
	}
	if d.frag += len(payload); !d.open || d.frag > maxUnit {
		d.H264Packet, d.open = codecs.H264Packet{}, false
		return []byte{}, nil
	}
	if payload[1]&0x40 != 0 {
		d.open = false
	}
	return d.H264Packet.Unmarshal(payload)
}

// timed is a picture with its stream timestamp; lost: pictures before it
// were lost or broken (viewers need a keyframe).
type timed struct {
	picture
	ts      uint64
	arrival int64 // µs since the session began
	lost    bool
}

func (v *videoIn) push(p *rtp.Packet, arrival int64) []timed {
	if !v.clock.started { // pictures come out a packet later
		v.clock.micros(p.Timestamp, arrival)
	}
	v.sb.Push(p)
	var out []timed
	for s := v.sb.Pop(); s != nil; s = v.sb.Pop() {
		ts := v.clock.micros(s.PacketTimestamp, arrival)
		pic, err := v.avc.unit(s.Data)
		if err != nil || len(s.Data) == 0 || len(s.Data) > maxUnit {
			out = append(out, timed{lost: true})
			continue
		}
		out = append(out, timed{picture: pic, ts: ts, arrival: arrival, lost: s.PrevDroppedPackets > 0})
	}
	return out
}

// feed passes a WHIP session's media to the stream hub: the stream starts
// with the first keyframe (its SPS gives the size) and changes with new
// sizes. Pictures come from one goroutine, sound from another.
type feed struct {
	hub                *stream.Hub
	session            atomic.Uint32 // the owner's voice session; 0: waiting for one (see orphan)
	audio              string        // "opus" or ""
	hooks              stream.IngestHooks
	bytes              *rate.Limiter
	sounds             *rate.Limiter
	now                func() time.Time
	config             stream.Config // the pictures' goroutine's
	ingest             atomic.Pointer[stream.Ingest]
	sClock             clock   // the sound goroutine's
	av                 avSync  // pictures on the sound's timeline
	srSound, srPicture srClock // the sender's reports (see senderreport.go)
	synced             int64   // the pictures': when the sync was logged last (arrival, µs)
	// The stream starts and stops under mu: a stop while it starts wins.
	mu      sync.Mutex
	stopped bool
	// still makes a JPEG of a keyframe for the preview (nil: none); one at
	// a time, at most every previewEvery (previewAt: the pictures').
	still      func(ctx context.Context, keyframe []byte) ([]byte, error)
	previewAt  time.Time
	previewing atomic.Bool
	// warn tells the owner about a problem of the stream (nil: nobody);
	// bWarned: about B-frames, once (the pictures').
	warn    func(session uint32, text string)
	bWarned bool
	// done ends a preview in the making when the stream stops (a late one
	// would land on the session's next stream).
	done   context.Context
	cancel context.CancelFunc
}

const (
	previewEvery   = 15 * time.Second // as browsers send theirs
	previewTimeout = 20 * time.Second
)

// previewDue: a keyframe now makes a preview.
func previewDue(last, now time.Time, busy bool) bool {
	return !busy && now.Sub(last) >= previewEvery
}

// makePreview makes a keyframe the stream's preview (a failure keeps the
// one before).
func (f *feed) makePreview(key []byte) {
	defer f.previewing.Store(false)
	ctx, cancel := context.WithTimeout(f.done, previewTimeout)
	defer cancel()
	pic, err := f.still(ctx, key)
	if err == nil && f.done.Err() == nil {
		err = f.hub.SetPreview(f.session.Load(), "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(pic))
	}
	if err != nil {
		slog.Debug("no preview of a stream from OBS", "session", f.session.Load(), "err", err)
	}
}

// bFramesNote: what the owner reads (see the web app's OBS dialog).
const bFramesNote = `Your OBS stream has B-frames, so the picture breaks up for viewers. In OBS, Settings → Output: with x264 type "bframes=0" in x264 Options; with NVENC, QuickSync or AMF set B-Frames to 0. Then start the stream again.`

const soundsPerSec = 100 // as from a stream connection

func newFeed(hub *stream.Hub, session uint32, audio bool, bytesPerSec int, hooks stream.IngestHooks) *feed {
	f := &feed{hub: hub, hooks: hooks, now: time.Now, sClock: clock{rate: 48_000},
		bytes:  rate.NewLimiter(rate.Limit(bytesPerSec), 2*bytesPerSec),
		sounds: rate.NewLimiter(soundsPerSec, soundsPerSec/2)}
	if audio {
		f.audio = "opus"
	}
	f.session.Store(session)
	f.done, f.cancel = context.WithCancel(context.Background())
	return f
}

// orphan: the hub ended the stream (its voice session left, moved, or lost
// Speak). Pictures wait until rebind gives the owner's voice session again
// (a browser refresh is a new session); the next keyframe starts the stream
// there. Called under the hub's lock (an Ended hook).
func (f *feed) orphan() {
	f.session.Store(0)
	f.ingest.Store(nil)
}

func (f *feed) rebind(voice uint32) { f.session.Store(voice) }

// waiting: for a voice session, after orphan.
func (f *feed) waiting() bool { return f.session.Load() == 0 }

// live is the stream's ingest, nil once the hub ended it (an end can come
// between StartIngest and its store in start).
func (f *feed) live() *stream.Ingest {
	in := f.ingest.Load()
	if in != nil && in.Ended() {
		f.ingest.CompareAndSwap(in, nil)
		return nil
	}
	return in
}

// started: the stream is live (it had its first keyframe).
func (f *feed) started() bool { return f.live() != nil }

// picture forwards a picture; an error ends the session (the stream
// cannot start, or a new size is refused).
func (f *feed) picture(t timed) error {
	in := f.live()
	if t.lost && in != nil {
		in.Dropped()
	}
	if t.data == nil {
		return nil
	}
	if t.b && !f.bWarned && f.warn != nil {
		if session := f.session.Load(); session != 0 {
			f.bWarned = true
			slog.Info("stream from OBS has B-frames", "session", session)
			f.warn(session, bFramesNote)
		}
	}
	if f.audio != "" {
		t.ts = uint64(f.av.picture(int64(t.ts), t.arrival))
		f.logSync(t.arrival)
	}
	changed := false
	if t.codec != "" {
		c := stream.Config{Codec: t.codec, Width: t.width, Height: t.height, Audio: f.audio}
		changed, f.config = c != f.config, c
	}
	switch {
	case in == nil && (!t.key || f.config.Codec == "" || f.waiting()):
		return nil // waiting for a keyframe (asked for, see session), or for the owner
	case in == nil:
		var err error
		if in, err = f.start(); err != nil || in == nil {
			return err
		}
	case changed:
		if err := in.Configure(f.config); err != nil {
			if in.Ended() { // orphaned meanwhile: the next keyframe starts it again
				return nil
			}
			return sizeError(err, f.config)
		}
	}
	if !f.bytes.AllowN(f.now(), len(t.data)) {
		in.Dropped()
		return nil
	}
	in.Frame(t.key, t.ts, t.data)
	if now := f.now(); t.key && f.still != nil && previewDue(f.previewAt, now, f.previewing.Load()) {
		f.previewAt = now
		f.previewing.Store(true)
		go f.makePreview(bytes.Clone(t.data))
	}
	return nil
}

// syncLogEvery: how often the A/V correction is logged (to find streams
// out of sync, see meta/issues/whip-av-sync.md).
const syncLogEvery = 10_000_000 // µs

func (f *feed) logSync(arrival int64) {
	if arrival-f.synced < syncLogEvery {
		return
	}
	f.synced = arrival
	if st := f.av.state(); st.known {
		args := []any{"session", f.session.Load(), "sound_drift_ms", st.sound / 1000, "picture_drift_ms", st.pictures / 1000, "shift_ms", st.shift / 1000}
		if e, ok := avError(&f.srSound, &f.srPicture); ok { // what viewers get: the picture later than its sound by this
			args = append(args, "picture_late_ms", (e-st.shift)/1000)
		}
		slog.Info("stream from OBS: sync", args...)
	}
}

func sizeError(err error, c stream.Config) error {
	return fmt.Errorf("%w (the picture: %dx%d, %s)", err, c.Width, c.Height, c.Codec)
}

// sound forwards an Opus packet (once the stream is live).
func (f *feed) sound(p *rtp.Packet, arrival int64) {
	ts := f.sClock.micros(p.Timestamp, arrival)
	f.srSound.apply(&f.sClock)
	f.av.noteSound(int64(ts), arrival)
	if in := f.ingest.Load(); in != nil && len(p.Payload) > 0 && f.sounds.Allow() {
		in.Sound(ts, p.Payload)
	}
}

// stop ends the stream, if it started.
func (f *feed) stop() {
	f.cancel()
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	if in := f.ingest.Load(); in != nil {
		in.Stop()
	}
}

func (f *feed) start() (*stream.Ingest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil, errors.New("stopped")
	}
	voice := f.session.Load()
	if voice == 0 { // orphaned meanwhile: not yet
		return nil, nil
	}
	in, err := f.hub.StartIngest(voice, f.config, f.hooks)
	if err != nil {
		return nil, sizeError(err, f.config)
	}
	f.ingest.Store(in)
	f.previewAt = time.Time{} // a preview for the new stream soon
	slog.Info("stream from OBS is live", "session", voice, "codec", f.config.Codec, "width", f.config.Width, "height", f.config.Height, "audio", f.config.Audio)
	return in, nil
}
