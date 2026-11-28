package recording

import (
	"encoding/binary"
	"errors"
	"os"
)

const (
	// libopus's encoder delay at 48 kHz: the same in every track, so
	// they stay aligned.
	preSkip = 312
	// A burst continues while packets come at most this far apart and
	// their sequence numbers stay this close to the clock; else it starts
	// anew where the clock says.
	burstGap   = sampleRate
	burstDrift = 2 * sampleRate
)

// track is one speaker's Ogg Opus file. Every track starts at the
// recording's start; silence packets fill the time the speaker was quiet,
// so the tracks line up in any editor. Positions are in samples (48 kHz)
// since the start.
type track struct {
	name   string
	f      *os.File
	ogg    oggWriter
	stereo bool
	pos    int64 // written up to here
	first  int64 // the first words
	// The burst: sequence number baseSeq goes to base; last packet's
	// sequence number and arrival.
	talking bool
	base    int64
	baseSeq uint64
	lastSeq uint64
	lastAt  int64
	err     error
}

func opusHead(channels byte) []byte {
	b := append([]byte("OpusHead"), 1, channels)
	b = binary.LittleEndian.AppendUint16(b, preSkip)
	b = binary.LittleEndian.AppendUint32(b, sampleRate)
	return append(b, 0, 0, 0) // gain, mapping family 0
}

func opusTags(title string) []byte {
	const vendor = "Kallisti"
	b := binary.LittleEndian.AppendUint32([]byte("OpusTags"), uint32(len(vendor)))
	b = append(b, vendor...)
	comment := "TITLE=" + title
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(comment)))
	return append(b, comment...)
}

// createTrack writes the headers (mono until a stereo packet comes).
func createTrack(path, name string, serial uint32) (*track, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- the recording's dir and a safeName (fileFor)
	if err != nil {
		return nil, err
	}
	t := &track{name: name, f: f, ogg: oggWriter{w: f, serial: serial}, first: -1}
	if err := errors.Join(t.ogg.page([][]byte{opusHead(1)}, 0, flagBOS), t.ogg.page([][]byte{opusTags(name)}, 0, 0)); err != nil {
		f.Close()
		return nil, err
	}
	return t, nil
}

// errSizeLimit: the silence before a packet would take more bytes than
// are left.
var errSizeLimit = errors.New("size limit")

// add writes a packet that arrived at "at", with silence before it, if
// that silence takes at most room bytes (else errSizeLimit, nothing
// written). It returns the first write error (the track writes nothing
// after it).
func (t *track) add(opus []byte, seq uint64, terminator bool, at, room int64) error {
	samples, stereo, ok := packetSamples(opus)
	if !ok || t.err != nil {
		return t.err
	}
	base, baseSeq := t.base, t.baseSeq
	pos := base + int64(seq-baseSeq)*frameSamples
	switch {
	case t.talking && seq <= t.lastSeq && t.lastSeq-seq < 50 && at-t.lastAt <= burstGap:
		return nil // a repeat or a late packet
	case !t.talking || seq <= t.lastSeq || at-t.lastAt > burstGap || pos > at+burstDrift || pos < at-burstDrift:
		base, baseSeq = at/grid*grid, seq
		pos = base
	}
	if silenceBytes(pos-t.pos) > room {
		return errSizeLimit
	}
	t.base, t.baseSeq = base, baseSeq
	t.talking, t.lastSeq, t.lastAt = !terminator, seq, at
	if stereo && !t.stereo {
		t.stereo = true
		_, t.err = t.f.WriteAt(buildPage([][]byte{opusHead(2)}, 0, t.ogg.serial, 0, flagBOS), 0)
	}
	t.fill(pos)
	if t.first < 0 {
		t.first = t.pos
	}
	t.write(opus, samples)
	return t.err
}

func (t *track) write(p []byte, samples int) {
	if t.err == nil {
		t.pos += int64(samples)
		t.err = t.ogg.packet(p, t.pos)
	}
}

// silenceBytes: what n samples of silence take, at most.
func silenceBytes(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return (n + sampleRate - 1) / sampleRate * padBytes
}

// fill writes silence up to pos.
func (t *track) fill(pos int64) {
	if pos > t.pos {
		for p := range silence(int(pos-t.pos), t.stereo) {
			t.write(p, frameSize(p[0]>>3))
		}
	}
}

// finish fills silence up to end and closes the file.
func (t *track) finish(end int64) error {
	t.fill(end)
	if t.err == nil {
		t.err = t.ogg.close()
	}
	return errors.Join(t.err, t.f.Close())
}

// bytes: the file's size so far, with what is buffered.
func (t *track) bytes() int64 { return t.ogg.size() }
