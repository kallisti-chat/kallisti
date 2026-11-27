package mumbled

import (
	"net"
	"testing"

	"kallisti.chat/server/internal/mumble"
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
	b.ReportAllocs()
	for b.Loop() {
		typ, body, err := readFrame(conn, maxFrame)
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
