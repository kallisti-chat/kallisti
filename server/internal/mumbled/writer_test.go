package mumbled

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"kallisti.chat/server/internal/certs"
)

// keepOpen lets one connection serve many runs of the writer, which
// closes it at the end of each.
type keepOpen struct{ net.Conn }

func (keepOpen) Close() error { return nil }

// tlsPair is a TLS connection over loopback, as native clients have.
func tlsPair(b *testing.B) (server, client net.Conn) {
	certPEM, keyPEM, err := certs.GenerateServer("bench")
	if err != nil {
		b.Fatal(err)
	}
	cert, err := certs.Load(certPEM, keyPEM)
	if err != nil {
		b.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		s := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}})
		s.Handshake()
		accepted <- s
	}()
	client, err = tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		b.Fatal(err)
	}
	server = <-accepted
	b.Cleanup(func() { server.Close(); client.Close() })
	return server, client
}

func pipePair(b *testing.B) (server, client net.Conn) {
	server, client = net.Pipe()
	b.Cleanup(func() { server.Close(); client.Close() })
	return server, client
}

// The writer draining voice frames that are already queued, as when it
// fell behind.
func BenchmarkWriterQueuedVoice(b *testing.B) {
	frame := make([]byte, 70) // a small Opus packet with its headers
	for _, path := range []struct {
		name string
		pair func(*testing.B) (net.Conn, net.Conn)
	}{{"tls", tlsPair}, {"pipe", pipePair}} {
		for _, n := range []int{1, 4, 32} {
			b.Run(fmt.Sprintf("%s/frames=%d", path.name, n), func(b *testing.B) {
				srv, peer := path.pair(b)
				c := &client{conn: keepOpen{srv}, out: make(chan []byte, 1), voice: make(chan []byte, n)}
				got := make([]byte, n*len(frame))
				read := make(chan error)
				go func() {
					for {
						_, err := io.ReadFull(peer, got)
						read <- err
						if err != nil {
							return
						}
					}
				}()
				b.ReportAllocs()
				b.SetBytes(int64(len(got)))
				b.ResetTimer()
				for range b.N {
					for range n {
						c.send(frame, true)
					}
					c.closeAfterFlush()
					c.writer()
					if err := <-read; err != nil {
						b.Fatal(err)
					}
					c.closeOnce = sync.Once{}
				}
			})
		}
	}
}

// recorder records each write, as the client's connection.
type recorder struct {
	net.Conn  // unused: only the methods below are called
	writes    [][]byte
	given     [][]byte // the slices as written, to see what was copied
	deadlines []time.Time
	onWrite   func(b []byte)
	err       error
	closed    chan struct{}
}

func newRecorder() *recorder { return &recorder{closed: make(chan struct{})} }

func (r *recorder) Write(b []byte) (int, error) {
	r.writes = append(r.writes, bytes.Clone(b))
	r.given = append(r.given, b)
	if r.onWrite != nil {
		r.onWrite(b)
	}
	if r.err != nil {
		return 0, r.err
	}
	return len(b), nil
}

func (r *recorder) SetWriteDeadline(t time.Time) error {
	r.deadlines = append(r.deadlines, t)
	return nil
}

func (r *recorder) Close() error {
	close(r.closed)
	return nil
}

func (r *recorder) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-r.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("not closed")
	}
}

func (r *recorder) strings() []string {
	var s []string
	for _, w := range r.writes {
		s = append(s, string(w))
	}
	return s
}

func recordedClient(r *recorder) *client {
	return &client{conn: r, out: make(chan []byte, queueSize), voice: make(chan []byte, queueSize)}
}

func TestQueuedFramesGoOutInOneWrite(t *testing.T) {
	r := newRecorder()
	c := recordedClient(r)
	c.send([]byte("a"), false)
	c.send([]byte("b"), false)
	c.send([]byte("v"), true)
	c.send([]byte("w"), true)
	c.closeAfterFlush()
	start := time.Now()
	c.writer()
	// Voice first, then the other frames, each queue in order.
	if got := r.strings(); !slices.Equal(got, []string{"vwab"}) {
		t.Fatalf("writes %q", got)
	}
	// One deadline for the write, the client's own.
	if len(r.deadlines) != 1 || r.deadlines[0].Before(start.Add(writeTimeout)) {
		t.Fatalf("deadlines %v", r.deadlines)
	}
	r.waitClosed(t)
}

// The writer does not wait for more: a lone frame goes out by itself,
// not copied.
func TestALoneFrameIsWrittenAlone(t *testing.T) {
	r := newRecorder()
	wrote := make(chan struct{}, 1)
	r.onWrite = func([]byte) { wrote <- struct{}{} }
	c := recordedClient(r)
	frame := []byte("a")
	c.send(frame, false)
	go c.writer()
	select {
	case <-wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("not written")
	}
	c.closeAfterFlush()
	r.waitClosed(t)
	if got := r.strings(); !slices.Equal(got, []string{"a"}) || &r.given[0][0] != &frame[0] {
		t.Fatalf("writes %q", got)
	}
}

func TestTheCloseMarkerStopsTheDrain(t *testing.T) {
	r := newRecorder()
	c := recordedClient(r)
	c.send([]byte("a"), false)
	c.send([]byte("v"), true)
	c.closeAfterFlush()
	c.send([]byte("late"), false)
	c.writer()
	if got := r.strings(); !slices.Equal(got, []string{"va"}) {
		t.Fatalf("writes %q", got)
	}
	r.waitClosed(t)
}

// An avatar is sent as it is, and voice that comes meanwhile goes next.
func TestABigFrameIsWrittenOnItsOwn(t *testing.T) {
	r := newRecorder()
	c := recordedClient(r)
	big := bytes.Repeat([]byte("B"), maxFrame)
	r.onWrite = func(b []byte) {
		if len(b) == len(big) {
			c.send([]byte("v"), true)
		}
	}
	c.send([]byte("a"), false)
	c.send(big, false)
	c.send([]byte("c"), false)
	c.closeAfterFlush()
	c.writer()
	got := r.strings()
	if len(got) != 3 || got[0] != "a" || got[1] != string(big) || got[2] != "vc" {
		t.Fatalf("writes %d: %.10q", len(got), got)
	}
	if &r.given[1][0] != &big[0] {
		t.Fatal("the big frame was copied")
	}
}

func TestBatchesAreCapped(t *testing.T) {
	r := newRecorder()
	c := recordedClient(r)
	r.onWrite = func([]byte) {
		if len(r.writes) == 1 {
			c.send([]byte("v"), true)
		}
	}
	var want []byte
	for i := range 40 {
		frame := bytes.Repeat([]byte{byte('0' + i%10)}, 1000)
		want = append(want, frame...)
		c.send(frame, false)
	}
	c.closeAfterFlush()
	c.writer()
	if len(r.writes) != 3 {
		t.Fatalf("%d writes", len(r.writes))
	}
	for _, w := range r.writes {
		if len(w) > maxBatch {
			t.Fatalf("a write of %d bytes", len(w))
		}
	}
	// Voice that came during the first batch leads the second.
	if r.writes[1][0] != 'v' {
		t.Fatalf("second write starts %q", r.writes[1][:1])
	}
	got := slices.Concat(r.writes[0], r.writes[1][1:], r.writes[2])
	if !bytes.Equal(got, want) {
		t.Fatal("frames out of order")
	}
}

// Voice held back by the cap goes before voice that came after it.
func TestVoiceKeepsItsOrderAcrossBatches(t *testing.T) {
	r := newRecorder()
	c := recordedClient(r)
	r.onWrite = func([]byte) {
		if len(r.writes) == 1 {
			c.send([]byte("v"), true)
		}
	}
	var want []byte
	for i := range 20 {
		frame := bytes.Repeat([]byte{byte('0' + i%10)}, 1000)
		want = append(want, frame...)
		c.send(frame, true)
	}
	c.send([]byte("a"), false)
	c.closeAfterFlush()
	c.writer()
	want = append(want, "va"...)
	if got := bytes.Join(r.writes, nil); len(r.writes) != 2 || !bytes.Equal(got, want) {
		t.Fatalf("%d writes, in order: %v", len(r.writes), bytes.Equal(got, want))
	}
}

func TestAWriteErrorEndsTheWriter(t *testing.T) {
	r := newRecorder()
	r.err = errors.New("broken pipe")
	c := recordedClient(r)
	c.send([]byte("a"), false)
	c.send([]byte("b"), false)
	done := make(chan struct{})
	go func() {
		c.writer()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer goes on")
	}
	r.waitClosed(t)
	if got := r.strings(); !slices.Equal(got, []string{"ab"}) {
		t.Fatalf("writes %q", got)
	}
}
