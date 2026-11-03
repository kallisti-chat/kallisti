package whip

import (
	"bytes"
	"errors"
	"fmt"
)

// H.264 as viewers decode it (WebCodecs, see the web app's stream
// protocol): Annex B access units, the parameter sets in front of each
// keyframe (no decoder description), keyframes flagged.

const (
	nalSlice = 1
	nalIDR   = 5
	nalSPS   = 7
	nalPPS   = 8
)

// nals splits Annex B data into NAL units (without start codes).
func nals(b []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+2 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 || b[i+2] != 1 {
			continue
		}
		if start >= 0 {
			if n := bytes.TrimRight(b[start:i], "\x00"); len(n) > 0 {
				out = append(out, n)
			}
		}
		i += 2
		start = i + 1
	}
	if start >= 0 && start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// picture is an access unit to forward. codec, width, height: from its
// SPS ("" without one). b: it has a B slice (viewers decode pictures in
// the order they come, so B-frames break the picture).
type picture struct {
	data          []byte
	key, b        bool
	codec         string
	width, height int
}

// avc keeps the last parameter sets, for keyframes that come without them.
type avc struct{ sps, pps []byte }

func (a *avc) unit(au []byte) (picture, error) {
	var p picture
	var sps, pps bool
	for _, n := range nals(au) {
		switch n[0] & 0x1f {
		case nalSPS:
			w, h, codec, err := parseSPS(n)
			if err != nil {
				return picture{}, err
			}
			a.sps, sps, p.codec, p.width, p.height = bytes.Clone(n), true, codec, w, h
		case nalPPS:
			a.pps, pps = bytes.Clone(n), true
		case nalIDR:
			p.key = true
		case nalSlice:
			p.b = p.b || bSlice(n)
		}
	}
	p.data = au
	if p.key && !(sps && pps) {
		if a.sps == nil || a.pps == nil {
			return picture{}, errors.New("a keyframe before the parameter sets")
		}
		p.data = append(annexB(a.sps, a.pps), au...)
	}
	return p, nil
}

// bSlice: the slice header's slice_type is B (1, or 6: all slices B).
func bSlice(nal []byte) bool {
	r := &bits{b: unescape(nal[1:])}
	r.ue() // first_mb_in_slice
	t := r.ue()
	return r.err == nil && t%5 == 1
}

// annexB joins NAL units with 4-byte start codes.
func annexB(units ...[]byte) []byte {
	var out []byte
	for _, u := range units {
		out = append(append(out, 0, 0, 0, 1), u...)
	}
	return out
}

// parseSPS reads the picture size and the WebCodecs codec name
// (avc1.PPCCLL) from a sequence parameter set.
func parseSPS(nal []byte) (width, height int, codec string, err error) {
	if len(nal) < 4 || nal[0]&0x1f != nalSPS {
		return 0, 0, "", errors.New("not an SPS")
	}
	r := &bits{b: unescape(nal[4:])}
	profile := nal[1]
	codec = fmt.Sprintf("avc1.%02x%02x%02x", nal[1], nal[2], nal[3])
	r.ue() // seq_parameter_set_id
	chroma := uint32(1)
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		if chroma = r.ue(); chroma == 3 {
			if r.u(1) == 1 { // separate colour planes: as monochrome
				chroma = 0
			}
		}
		r.ue() // bit depths
		r.ue()
		r.u(1)
		if r.u(1) == 1 { // scaling matrices
			n := 8
			if chroma == 3 {
				n = 12
			}
			for i := range n {
				if r.u(1) == 1 {
					size := 16
					if i >= 6 {
						size = 64
					}
					r.scalingList(size)
				}
			}
		}
	}
	r.ue() // log2_max_frame_num_minus4
	switch r.ue() {
	case 0:
		r.ue()
	case 1:
		r.u(1)
		r.se()
		r.se()
		for n := r.ue(); n > 0 && r.err == nil; n-- {
			r.se()
		}
	}
	r.ue() // max_num_ref_frames
	r.u(1) // gaps_in_frame_num_value_allowed_flag
	mbW, mapH := r.ue()+1, r.ue()+1
	frameMBsOnly := r.u(1)
	if frameMBsOnly == 0 {
		r.u(1)
	}
	r.u(1) // direct_8x8_inference_flag
	var left, right, top, bottom uint32
	if r.u(1) == 1 {
		left, right, top, bottom = r.ue(), r.ue(), r.ue(), r.ue()
	}
	if r.err != nil {
		return 0, 0, "", errors.New("a short SPS")
	}
	fields := 2 - frameMBsOnly
	cropX, cropY := uint32(1), fields
	switch chroma {
	case 1:
		cropX, cropY = 2, 2*fields
	case 2:
		cropX = 2
	}
	w, h := int64(mbW)*16-int64(cropX)*int64(left+right), int64(fields)*int64(mapH)*16-int64(cropY)*int64(top+bottom)
	if w <= 0 || h <= 0 || w > 1<<16 || h > 1<<16 {
		return 0, 0, "", errors.New("a bad picture size in the SPS")
	}
	return int(w), int(h), codec, nil
}

// unescape removes emulation prevention bytes (00 00 03 → 00 00).
func unescape(b []byte) []byte {
	out := make([]byte, 0, len(b))
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

// bits reads big-endian bits and Exp-Golomb codes; reading past the end
// sets err (and gives zeros).
type bits struct {
	b   []byte
	pos int
	err error
}

func (r *bits) u(n int) uint32 {
	var v uint32
	for range n {
		if r.pos >= len(r.b)*8 {
			r.err = errors.New("short")
			return 0
		}
		v = v<<1 | uint32(r.b[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v
}

func (r *bits) ue() uint32 {
	zeros := 0
	for r.u(1) == 0 && r.err == nil {
		if zeros++; zeros > 31 {
			r.err = errors.New("bad Exp-Golomb code")
			return 0
		}
	}
	return 1<<zeros - 1 + r.u(zeros)
}

func (r *bits) se() int32 {
	v := r.ue()
	if v%2 == 1 {
		return int32(v/2 + 1)
	}
	return -int32(v / 2)
}

func (r *bits) scalingList(size int) {
	last, next := int32(8), int32(8)
	for range size {
		if next != 0 {
			next = (last + r.se() + 256) % 256
		}
		if next != 0 {
			last = next
		}
	}
}
