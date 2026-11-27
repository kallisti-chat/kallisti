package mumbled

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// voiceFrame is a tunnelled voice frame with an 80-byte body, the size of
// a 20 ms Opus packet at voice bitrates.
func voiceFrame(frameNumber uint64) []byte {
	packet := audio(frameNumber%100, make([]byte, 73))
	if len(packet) != 80 {
		panic("the voice packet is not 80 bytes")
	}
	return mumble.EncodeFrame(mumble.TypeUDPTunnel, packet)
}

// repeatConn plays the same bytes again and again, without a socket.
type repeatConn struct {
	net.Conn
	data []byte
	off  int
}

func (r *repeatConn) Read(p []byte) (int, error) {
	n := copy(p, r.data[r.off:])
	r.off = (r.off + n) % len(r.data)
	return n, nil
}

// benchmarkReadVoice reads voice frames from conn as a client's reader
// does, and decodes each as the voice path does.
func benchmarkReadVoice(b *testing.B, conn net.Conn) {
	frames := newFrameReader(conn)
	b.ReportAllocs()
	for b.Loop() {
		typ, body, err := frames.read(maxFrame)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := decodeAudio(body, false); typ != mumble.TypeUDPTunnel || !ok {
			b.Fatal("not a voice frame")
		}
	}
}

// BenchmarkReadVoice reads 80-byte tunnelled voice frames from memory and
// over a net.Pipe, which is how web clients reach the server. Over the
// pipe each frame is its own write, as the web bridge sends them.
func BenchmarkReadVoice(b *testing.B) {
	b.Run("memory", func(b *testing.B) {
		var data []byte
		for i := range 64 {
			data = append(data, voiceFrame(uint64(i))...)
		}
		benchmarkReadVoice(b, &repeatConn{data: data})
	})
	b.Run("pipe", func(b *testing.B) {
		client, server := net.Pipe()
		defer server.Close()
		defer client.Close()
		frames := make([][]byte, 64)
		for i := range frames {
			frames[i] = voiceFrame(uint64(i))
		}
		go func() {
			for i := 0; ; i++ {
				if _, err := client.Write(frames[i%len(frames)]); err != nil {
					return
				}
			}
		}()
		benchmarkReadVoice(b, server)
	})
}

// joinWithMore joins as name and sends more frames in the same write as
// Version and Authenticate, over TLS or, as web clients, over a pipe.
func joinWithMore(t *testing.T, s *Server, addr string, pipe bool, name string, more []byte) *testClient {
	t.Helper()
	var conn net.Conn
	if pipe {
		a, b := net.Pipe()
		go s.ServeConn(b, Origin{Host: netip.MustParseAddr("203.0.113.5")})
		conn = a
	} else {
		tc, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		conn = tc
	}
	c := startClientReader(t, conn)
	v1, v2 := uint32(1<<16|5<<8), uint64(1<<48|5<<32)
	b := mumble.MessageFrame(mumble.TypeVersion, &pb.Version{VersionV1: &v1, VersionV2: &v2})
	b = append(b, mumble.MessageFrame(mumble.TypeAuthenticate, &pb.Authenticate{Username: proto.String(name), Opus: proto.Bool(true)})...)
	if _, err := conn.Write(append(b, more...)); err != nil {
		t.Fatal(err)
	}
	return joined(t, c)
}

// eachTransport runs a test over TLS and over a pipe.
func eachTransport(t *testing.T, f func(t *testing.T, s *Server, addr string, pipe bool)) {
	for _, pipe := range []bool{false, true} {
		name := "tls"
		if pipe {
			name = "pipe"
		}
		t.Run(name, func(t *testing.T) {
			s, addr := startServerAndListen(t, "127.0.0.1", Config{}, nil)
			f(t, s, addr, pipe)
		})
	}
}

func expectVoice(t *testing.T, c *testClient, from uint32, frameNumber uint64, opus []byte) {
	t.Helper()
	h := parseAudio(t, c.expect(mumble.TypeUDPTunnel))
	if h.sender != from || h.frame != frameNumber || !bytes.Equal(h.opus, opus) {
		t.Fatalf("heard frame %d from %d (%d bytes), want frame %d from %d (%d bytes)",
			h.frame, h.sender, len(h.opus), frameNumber, from, len(opus))
	}
}

func expectPong(t *testing.T, c *testClient, timestamp uint64) {
	t.Helper()
	var pong pb.Ping
	proto.Unmarshal(c.expect(mumble.TypePing), &pong)
	if pong.GetTimestamp() != timestamp {
		t.Fatalf("pong %d, want %d", pong.GetTimestamp(), timestamp)
	}
}

func expectClosed(t *testing.T, c *testClient) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-c.frames:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("the connection is still open")
		}
	}
}

// Clients may send frames right behind Authenticate: none is lost, and
// all are handled in order.
func TestFramesBehindAuthenticateAreHandled(t *testing.T) {
	eachTransport(t, func(t *testing.T, s *Server, addr string, pipe bool) {
		bob := dial(t, addr, "bob")
		var more []byte
		for i := range uint64(4) {
			more = append(more, mumble.MessageFrame(mumble.TypePing, &pb.Ping{Timestamp: proto.Uint64(100 + i)})...)
			more = append(more, mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(i+1, []byte{0xfc, byte(i)}))...)
		}
		alice := joinWithMore(t, s, addr, pipe, "alice", more)
		for i := range uint64(4) {
			expectPong(t, alice, 100+i)
			expectVoice(t, bob, alice.session, i+1, []byte{0xfc, byte(i)})
		}
	})
}

// Voice frames share a buffer: each must still reach listeners with its
// own bytes, also when they arrive back to back.
func TestBackToBackVoiceKeepsItsBytes(t *testing.T) {
	eachTransport(t, func(t *testing.T, s *Server, addr string, pipe bool) {
		bob := dial(t, addr, "bob")
		alice := joinWithMore(t, s, addr, pipe, "alice", nil)
		opus := func(i int) []byte { return bytes.Repeat([]byte{byte(i)}, 40+i) }
		var b []byte
		for i := range 30 {
			b = append(b, mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(uint64(i+1), opus(i)))...)
		}
		alice.conn.Write(b)
		for i := range 30 {
			expectVoice(t, bob, alice.session, uint64(i+1), opus(i))
		}
	})
}

// A voice frame larger than the shared buffer gets its own; the frames
// around it still use the shared one.
func TestVoiceLargerThanTheBuffer(t *testing.T) {
	eachTransport(t, func(t *testing.T, s *Server, addr string, pipe bool) {
		bob := dial(t, addr, "bob")
		alice := joinWithMore(t, s, addr, pipe, "alice", nil)
		small, large := []byte{0xfc, 1, 2}, bytes.Repeat([]byte{7}, 3*maxKeptVoice/2)
		if len(audio(2, large)) <= maxKeptVoice {
			t.Fatal("the large frame fits the buffer")
		}
		var b []byte
		b = append(b, mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(1, small))...)
		b = append(b, mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(2, large))...)
		b = append(b, mumble.EncodeFrame(mumble.TypeUDPTunnel, audio(3, []byte{0xfc, 3}))...)
		alice.conn.Write(b)
		expectVoice(t, bob, alice.session, 1, small)
		expectVoice(t, bob, alice.session, 2, large)
		expectVoice(t, bob, alice.session, 3, []byte{0xfc, 3})
	})
}

// Frames up to the limit are read; a larger one ends the connection, as
// it does in the handshake (with its own limit).
func TestFrameSizeLimits(t *testing.T) {
	header := func(typ uint16, n uint32) []byte {
		return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint16(nil, typ), n)
	}
	eachTransport(t, func(t *testing.T, s *Server, addr string, pipe bool) {
		alice := joinWithMore(t, s, addr, pipe, "alice", nil)
		// An unknown type is read and ignored.
		alice.conn.Write(append(header(0xfff0, maxFrame), make([]byte, maxFrame)...))
		alice.send(mumble.TypePing, &pb.Ping{Timestamp: proto.Uint64(1)})
		expectPong(t, alice, 1)
		alice.conn.Write(header(mumble.TypeUDPTunnel, maxFrame+1))
		expectClosed(t, alice)

		bob := joinWithMore(t, s, addr, pipe, "bob", nil)
		bob.conn.Write(header(mumble.TypePing, maxFrame+1))
		expectClosed(t, bob)
	})
	t.Run("handshake", func(t *testing.T) {
		s, _ := startServerAndListen(t, "127.0.0.1", Config{}, nil)
		a, b := net.Pipe()
		go s.ServeConn(b, Origin{Host: netip.MustParseAddr("203.0.113.5")})
		c := startClientReader(t, a)
		a.Write(header(mumble.TypeVersion, maxHandshakeFrame+1))
		expectClosed(t, c)
	})
}

// The idle timeout is a read deadline on the connection: a frame cut
// short and then silence still runs into it.
func TestATruncatedFrameRunsIntoTheReadDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	frames := newFrameReader(server)
	go func() {
		f := voiceFrame(1)
		client.Write(f)
		client.Write(f[:len(f)-10])
	}()
	server.SetReadDeadline(time.Now().Add(time.Second))
	if typ, body, err := frames.read(maxFrame); err != nil || typ != mumble.TypeUDPTunnel || len(body) != 80 {
		t.Fatalf("first frame: %d, %d bytes, %v", typ, len(body), err)
	}
	server.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := frames.read(maxFrame); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read %v, want the deadline", err)
	}
}
