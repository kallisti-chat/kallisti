package whip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"

	"kallisti.chat/server/internal/stream"
)

// voice: sessions 1 (the streamer) and 2 in channel 5; session 3 may not
// speak.
type voice struct {
	mu       sync.Mutex
	channels map[uint32]uint32
}

func (v *voice) Channel(s uint32) (uint32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.channels[s]
	return c, ok
}
func (v *voice) MaySpeak(s, _ uint32) bool { return s != 3 }

// viewer collects a viewer's frames: key flag, timestamp, data.
type viewer struct {
	mu     sync.Mutex
	frames []got
	texts  []map[string]any
}

type got struct {
	key, sound bool
	ts         uint64
	data       []byte
}

func (v *viewer) Room() bool { return true }

func (v *viewer) SendFrame(b []byte) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frames = append(v.frames, got{key: b[4]&1 != 0, sound: b[4]&2 != 0, ts: binary.BigEndian.Uint64(b[5:13]), data: b[13:]})
	return true
}
func (v *viewer) SendText(m any) {
	b, _ := json.Marshal(m)
	var t map[string]any
	json.Unmarshal(b, &t)
	v.mu.Lock()
	v.texts = append(v.texts, t)
	v.mu.Unlock()
}
func (v *viewer) Close() {}

func (v *viewer) pictures() []got {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []got
	for _, f := range v.frames {
		if !f.sound {
			out = append(out, f)
		}
	}
	return out
}

func (v *viewer) sounds() []got {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []got
	for _, f := range v.frames {
		if f.sound {
			out = append(out, f)
		}
	}
	return out
}

// hubWithViewer: session 2 watches session 1's stream as soon as it starts.
func hubWithViewer() (*stream.Hub, *voice, *viewer) {
	v := &voice{channels: map[uint32]uint32{1: 5, 2: 5, 3: 5}}
	h := stream.New(v)
	w := &viewer{}
	h.Connect(2, w)
	h.OnChange = func(infos []stream.Info) {
		if len(infos) > 0 {
			h.Watch(2, infos[0].Session)
		}
	}
	return h, v, w
}

// packets cuts access units into RTP packets as an encoder sends them
// (30 frames a second, the marker on each one's last packet).
func packets(frames [][]byte, seq uint16, ts uint32) []*rtp.Packet {
	var out []*rtp.Packet
	payloader := &codecs.H264Payloader{}
	for _, f := range frames {
		payloads := payloader.Payload(1200, f)
		for i, p := range payloads {
			out = append(out, &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 97, SequenceNumber: seq, Timestamp: ts, Marker: i == len(payloads)-1, SSRC: 1}, Payload: p})
			seq++
		}
		ts += 3000
	}
	return out
}

func clipFrames(t *testing.T) [][]byte {
	clip, err := os.ReadFile("testdata/1080p-high.h264")
	if err != nil {
		t.Fatal(err)
	}
	return splitFrames(clip)
}

func TestPicturesReachViewers(t *testing.T) {
	h, _, w := hubWithViewer()
	var keyframes int
	f := newFeed(h, 1, true, 1<<20, stream.IngestHooks{Keyframe: func() { keyframes++ }, Ended: func() {}})
	v := newVideoIn()
	frames := clipFrames(t)
	delta := frames[1]
	// A delta frame first (joined mid-stream): no stream yet.
	pkts := append(packets([][]byte{delta}, 100, 0), packets(append(frames, frames...), 101, 3000)...)
	for i, p := range pkts {
		for _, pic := range v.push(p, int64(i)*1000) {
			if err := f.picture(pic); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := w.pictures()
	// The last frame waits for a packet after it.
	if len(got) != 5 {
		t.Fatalf("%d pictures", len(got))
	}
	for i, g := range got {
		want := frames[i%3]
		if !bytes.Equal(g.data, want) || g.key != (i%3 != 1) {
			t.Errorf("picture %d: key %v, %d bytes, want %d bytes", i, g.key, len(g.data), len(want))
		}
		if i > 0 && g.ts-got[i-1].ts != 33_333 && g.ts-got[i-1].ts != 33_334 {
			t.Errorf("picture %d: %d µs after the one before", i, g.ts-got[i-1].ts)
		}
	}
	if w.texts[0]["codec"] != "avc1.640028" || w.texts[0]["width"] != 1920.0 || w.texts[0]["height"] != 1080.0 || w.texts[0]["audio"] != "opus" {
		t.Fatalf("stream %v", w.texts[0])
	}
	if !f.started() {
		t.Fatal("not started")
	}
}

func TestLostPacketsAskForAKeyframe(t *testing.T) {
	h, _, w := hubWithViewer()
	var keyframes int
	f := newFeed(h, 1, false, 1<<20, stream.IngestHooks{Keyframe: func() { keyframes++ }, Ended: func() {}})
	v := newVideoIn()
	frames := clipFrames(t)
	pkts := packets([][]byte{frames[0], frames[1], frames[1], frames[1]}, 0, 0)
	for i, p := range pkts {
		if i == len(pkts)-2 { // the third frame
			continue
		}
		for _, pic := range v.push(p, 0) {
			f.picture(pic)
		}
	}
	// Flush what waits.
	for _, pic := range v.push(&rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(len(pkts)), Timestamp: 99_000, Marker: true}, Payload: []byte{0x09, 0x10}}, 0) {
		f.picture(pic)
	}
	if n := len(w.pictures()); n != 2 {
		t.Fatalf("%d pictures reached the viewer (the broken one and after must not)", n)
	}
	if keyframes != 1 { // the viewer's first (then at most one a second)
		t.Fatalf("%d keyframe requests", keyframes)
	}
}

func TestSoundReachesViewers(t *testing.T) {
	h, _, w := hubWithViewer()
	f := newFeed(h, 1, true, 1<<20, stream.IngestHooks{Keyframe: func() {}, Ended: func() {}})
	opus := &rtp.Packet{Header: rtp.Header{Timestamp: 960}, Payload: []byte{0xfc, 0xff}}
	f.sound(opus, 0) // before the stream: dropped
	v := newVideoIn()
	for _, p := range packets(clipFrames(t), 0, 0) {
		for _, pic := range v.push(p, 0) {
			f.picture(pic)
		}
	}
	f.sound(&rtp.Packet{Header: rtp.Header{Timestamp: 960 + 48_000}, Payload: []byte{0xfc, 0xfe}}, 5000)
	s := w.sounds()
	if len(s) != 1 || !bytes.Equal(s[0].data, []byte{0xfc, 0xfe}) || s[0].ts != 1_000_000 {
		t.Fatalf("sounds %+v", s)
	}
}

func TestOverTheRateLimit(t *testing.T) {
	h, _, w := hubWithViewer()
	f := newFeed(h, 1, false, 400, stream.IngestHooks{Keyframe: func() {}, Ended: func() {}})
	v := newVideoIn()
	frames := clipFrames(t) // the first keyframe is larger than the burst, the second is not
	for _, p := range packets(append(frames, frames[1]), 0, 0) {
		for _, pic := range v.push(p, 0) {
			f.picture(pic)
		}
	}
	if got := w.pictures(); len(got) != 1 || !bytes.Equal(got[0].data, frames[2]) {
		t.Fatalf("%d pictures over the rate limit", len(got))
	}
}

func TestTooLargeOrNoSpeak(t *testing.T) {
	h, _, _ := hubWithViewer()
	f := newFeed(h, 3, false, 1<<20, stream.IngestHooks{Keyframe: func() {}, Ended: func() {}})
	v := newVideoIn()
	var err error
	for _, p := range packets(clipFrames(t), 0, 0) {
		for _, pic := range v.push(p, 0) {
			err = cmpErr(err, f.picture(pic))
		}
	}
	if err == nil {
		t.Fatal("a stream without Speak")
	}
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// The picture clock starts with the first packet's arrival, not when the
// first whole picture comes out (later: it waits for the next packet).
func TestPictureClockStartsWithTheFirstPacket(t *testing.T) {
	v := newVideoIn()
	var got []timed
	for i, p := range packets(clipFrames(t), 0, 0) {
		got = append(got, v.push(p, 1000+int64(i)*20_000)...)
	}
	if len(got) == 0 || got[0].ts != 1000 {
		t.Fatalf("first picture at %v", got)
	}
}

func TestClock(t *testing.T) {
	c := clock{rate: 90_000}
	if got := c.micros(4294967000, 1000); got != 1000 {
		t.Fatal(got)
	}
	if got := c.micros(4294967000+90_000-1<<32, 99_999); got != 1_001_000 { // across the wrap
		t.Fatal(got)
	}
	if got := c.micros(4294967000+45_000-1<<32, 0); got != 501_000 { // reordered
		t.Fatal(got)
	}
}

// Unfinished FU-A fragments (starts without ends, each on its own
// timestamp) must not pile up into the next picture.
func TestUnfinishedFragmentsAreDropped(t *testing.T) {
	v := newVideoIn()
	junk := append([]byte{0x7c, 0x85}, make([]byte, 1200)...) // FU-A start of an IDR slice
	for i := range 1000 {
		v.push(&rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(i), Timestamp: uint32(i) * 3000}, Payload: junk}, 0)
	}
	frames := clipFrames(t)
	var got []timed
	seq := uint16(1000)
	for i, f := range frames { // in fragments (a small MTU)
		payloads := (&codecs.H264Payloader{}).Payload(300, f)
		for j, p := range payloads {
			got = append(got, v.push(&rtp.Packet{Header: rtp.Header{SequenceNumber: seq, Timestamp: uint32(3_000_000 + 3000*i), Marker: j == len(payloads)-1}, Payload: p}, 0)...)
			seq++
		}
	}
	for _, g := range got {
		if g.key {
			if !bytes.Equal(g.data, frames[0]) {
				t.Fatalf("the keyframe has %d bytes, want %d", len(g.data), len(frames[0]))
			}
			return
		}
	}
	t.Fatal("no keyframe")
}

func TestFragmentsOverTheLimitAreDropped(t *testing.T) {
	d := &h264Depacketizer{}
	mid := append([]byte{0x7c, 0x05}, make([]byte, 1200)...)
	d.Unmarshal(append([]byte{0x7c, 0x85}, make([]byte, 1200)...))
	for range maxUnit / 1200 {
		d.Unmarshal(mid)
	}
	if out, _ := d.Unmarshal([]byte{0x7c, 0x45, 0}); len(out) != 0 {
		t.Fatalf("a %d-byte unit", len(out))
	}
	if out, _ := d.Unmarshal([]byte{0x65, 1, 2}); len(out) == 0 { // the next unit comes through
		t.Fatal("no unit after")
	}
}

// B-frames: the owner is told once (viewers decode in arrival order).
func TestBFramesWarnOnce(t *testing.T) {
	h, _, _ := hubWithViewer()
	f := newFeed(h, 1, false, 1<<20, stream.IngestHooks{Keyframe: func() {}, Ended: func() {}})
	var told []uint32
	f.warn = func(session uint32, text string) {
		if text == "" {
			t.Error("no text")
		}
		told = append(told, session)
	}
	f.picture(timed{picture: picture{data: []byte{1}}})
	for range 3 {
		f.picture(timed{picture: picture{data: []byte{1}, b: true}})
	}
	if len(told) != 1 || told[0] != 1 {
		t.Fatalf("told %v", told)
	}
}

// Normal network jitter or socket queuing causes bursts of frames.
// Ensure a burst of 30 frames does not trigger the fps rate limiter when within burst capacity.
func TestFramerateBurstTolerance(t *testing.T) {
	h, _, _ := hubWithViewer()
	f := newFeed(h, 1, false, 1<<20, stream.IngestHooks{Keyframe: func() {}, Ended: func() {}})
	f.limits = func() (int, int, int) { return 1 << 20, 4096, 120 }
	var warned bool
	f.warn = func(session uint32, text string) {
		warned = true
	}
	f.picture(timed{picture: picture{data: []byte{1}, key: true, width: 1280, height: 720, codec: "avc1.42001f"}})
	for range 30 {
		f.picture(timed{picture: picture{data: []byte{1}}})
	}
	if warned {
		t.Fatal("unexpected framerate warning on 30-frame jitter burst at 120fps limit")
	}
}
