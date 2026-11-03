package whip

import (
	"bytes"
	"encoding/hex"
	"os"
	"slices"
	"testing"
)

func unhex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// From x264 (ffmpeg): 1920x1080 High (coded 1088, cropped), 640x360
// Constrained Baseline (coded 368).
var (
	sps1080 = unhex("67640028acb200f0044fcb8088000003000800000301e078c19240")
	sps360  = unhex("6742c01ed900a02ff970110000030001000003003c0f162e48")
)

func TestParseSPS(t *testing.T) {
	for _, c := range []struct {
		sps           []byte
		width, height int
		codec         string
	}{{sps1080, 1920, 1080, "avc1.640028"}, {sps360, 640, 360, "avc1.42c01e"}} {
		w, h, codec, err := parseSPS(c.sps)
		if err != nil || w != c.width || h != c.height || codec != c.codec {
			t.Errorf("got %dx%d %s %v, want %dx%d %s", w, h, codec, err, c.width, c.height, c.codec)
		}
	}
	for _, bad := range [][]byte{nil, {0x67}, sps1080[:6], {0x68, 1, 2, 3}} {
		if _, _, _, err := parseSPS(bad); err == nil {
			t.Errorf("parsed %x", bad)
		}
	}
}

func TestNALs(t *testing.T) {
	got := nals(unhex("000000016742" + "0000016801" + "00000165aa0000"))
	want := [][]byte{unhex("6742"), unhex("6801"), unhex("65aa0000")}
	if len(got) != len(want) {
		t.Fatalf("got %x", got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("got %x", got)
		}
	}
}

func TestAccessUnits(t *testing.T) {
	var a avc
	idr, delta := unhex("65888202"), unhex("419a3b10")
	if _, err := a.unit(annexB(idr)); err == nil {
		t.Fatal("a keyframe without parameter sets")
	}
	p, err := a.unit(annexB(sps360, unhex("68cb83cb20"), idr))
	if err != nil || !p.key || p.width != 640 || p.codec != "avc1.42c01e" || !bytes.Equal(p.data, annexB(sps360, unhex("68cb83cb20"), idr)) {
		t.Fatalf("keyframe %+v %v", p, err)
	}
	p, _ = a.unit(annexB(delta))
	if p.key || p.codec != "" || !bytes.Equal(p.data, annexB(delta)) {
		t.Fatalf("delta %+v", p)
	}
	// A keyframe without them gets the last ones (a decoder may start there).
	p, _ = a.unit(annexB(idr))
	if !p.key || !bytes.Equal(p.data, annexB(sps360, unhex("68cb83cb20"), idr)) {
		t.Fatalf("keyframe %+v", p)
	}
}

func TestTheTestClip(t *testing.T) {
	clip, err := os.ReadFile("testdata/1080p-high.h264")
	if err != nil {
		t.Fatal(err)
	}
	if len(splitFrames(clip)) != 3 {
		t.Fatalf("frames %d", len(splitFrames(clip)))
	}
}

// splitFrames cuts an Annex-B clip into access units: each starts at an SPS
// or at a slice that follows a slice.
func splitFrames(clip []byte) [][]byte {
	var frames [][][]byte
	slice := false
	for _, n := range nals(clip) {
		typ := n[0] & 0x1f
		if len(frames) == 0 || typ == 7 || typ <= 5 && slice {
			frames = append(frames, nil)
			slice = false
		}
		frames[len(frames)-1] = append(frames[len(frames)-1], n)
		slice = slice || typ <= 5
	}
	out := make([][]byte, len(frames))
	for i, f := range frames {
		out[i] = annexB(f...)
	}
	return out
}

// Slice headers start with first_mb_in_slice and slice_type (Exp-Golomb):
// 0 and then I (2), P (0), B (1) or "all slices B" (6).
func TestBFrames(t *testing.T) {
	var a avc
	a.sps, a.pps = sps360, unhex("68ce3c80")
	for _, c := range []struct {
		au []byte
		b  bool
	}{
		{annexB([]byte{0x65, 0xb0}), false}, // IDR, I
		{annexB([]byte{0x41, 0xc0}), false}, // P
		{annexB([]byte{0x01, 0xa0}), true},  // B
		{annexB([]byte{0x01, 0x9c}), true},  // B (6)
		{annexB(sps360, []byte{0x41, 0xc0}), false},
		{annexB([]byte{0x41}), false}, // too short: not B
	} {
		if p, err := a.unit(c.au); err != nil || p.b != c.b {
			t.Errorf("%x: b %v %v, want %v", c.au, p.b, err, c.b)
		}
	}
}

// From x264 (ffmpeg, bframes=2): a keyframe, then P and B pictures.
func TestBFramesFromX264(t *testing.T) {
	clip, err := os.ReadFile("testdata/bframes.h264")
	if err != nil {
		t.Fatal(err)
	}
	var a avc
	var kinds []bool
	for _, f := range splitFrames(clip) {
		p, err := a.unit(f)
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, p.b)
	}
	if len(kinds) < 3 || kinds[0] || !slices.Contains(kinds, true) {
		t.Fatalf("B pictures %v", kinds)
	}
	for _, f := range clipFrames(t) { // without B-frames
		if p, _ := a.unit(f); p.b {
			t.Fatal("a B picture")
		}
	}
}
