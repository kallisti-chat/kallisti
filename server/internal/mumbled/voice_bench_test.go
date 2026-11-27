package mumbled

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"kallisti.chat/server/internal/cryptstate"
)

// BenchmarkVoiceToChannel is one speaker sending a voice packet to a
// channel of n listeners, without sockets. The queues are drained now and
// then (outside the timer), so no packet is dropped from a full queue.
func BenchmarkVoiceToChannel(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			const queue = 1024
			state := NewState("Root", nil)
			s := &Server{state: state, clients: map[uint32]*client{}}
			for session := uint32(1); session <= uint32(n)+1; session++ {
				u := &User{Session: session, Name: fmt.Sprint("user", session)}
				state.AddUser(u)
				state.Refresh(u)
				s.clients[session] = &client{
					session: session, seen: state.Visible(u),
					out: make(chan []byte, 1), voice: make(chan []byte, queue),
					bandwidth: rate.NewLimiter(rate.Inf, maxBandwidth),
				}
			}
			speaker := s.clients[1]
			packet := audio(1, make([]byte, 40))
			s.voice(speaker, packet, true)
			if len(s.clients[2].voice) != 1 || len(speaker.voice) != 0 {
				b.Fatal("the packet did not reach the listeners")
			}
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				s.voice(speaker, packet, true)
				if i%(queue-1) == queue-2 {
					b.StopTimer()
					for _, c := range s.clients {
						for len(c.voice) > 0 {
							<-c.voice
						}
					}
					b.StartTimer()
				}
			}
			for _, c := range s.clients {
				if c.dropped.Load() != 0 {
					b.Fatalf("%d voice frames dropped", c.dropped.Load())
				}
			}
		})
	}
}

// voiceListeners is a server with one speaker and n listeners that have a
// crypt state each, but no socket and no goroutine.
func voiceListeners(n int) (*Server, *client) {
	state := NewState("Root", nil)
	s := &Server{state: state, clients: map[uint32]*client{}}
	for session := uint32(1); session <= uint32(n)+1; session++ {
		u := &User{Session: session, Name: fmt.Sprint("user", session)}
		state.AddUser(u)
		state.Refresh(u)
		s.clients[session] = &client{
			session: session, seen: state.Visible(u),
			out: make(chan []byte, 1), voice: make(chan []byte, 1),
			bandwidth: rate.NewLimiter(rate.Inf, maxBandwidth),
			crypt:     cryptstate.New([16]byte{3}, [16]byte{2}, [16]byte{1}),
		}
	}
	return s, s.clients[1]
}

// udpListeners is voiceListeners with the listeners receiving over UDP on
// real loopback sockets. The server's socket is opened as the server opens
// it (dual stack, any address); the listeners' are udp4. Each listener
// socket is read by a goroutine, so the kernel never pushes back. One
// packet is sent first, and must reach every listener: deliver does not
// report a failed send, and a benchmark of failed sends says nothing.
// The counter is of the datagrams read; it is n after that first packet.
func udpListeners(b *testing.B, n int) (*Server, *client, *atomic.Int64) {
	received := new(atomic.Int64)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	s, speaker := voiceListeners(n)
	s.udp = conn
	for session := uint32(2); session <= uint32(n)+1; session++ {
		l, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			b.Fatal(err)
		}
		l.SetReadBuffer(4 << 20)
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 2048)
			for {
				if _, _, err := l.ReadFromUDPAddrPort(buf); err != nil {
					return
				}
				received.Add(1)
			}
		}()
		b.Cleanup(func() { l.Close(); <-done })
		c := s.clients[session]
		c.udpVoice, c.udpAddr = true, l.LocalAddr().(*net.UDPAddr).AddrPort()
	}
	s.voice(speaker, audio(1, make([]byte, 40)), false)
	for wait := time.Now().Add(5 * time.Second); received.Load() < int64(n); time.Sleep(time.Millisecond) {
		if time.Now().After(wait) {
			b.Fatalf("%d of %d listeners got the packet", received.Load(), n)
		}
	}
	return s, speaker, received
}

// BenchmarkVoiceToUDPListeners is one speaker sending a voice packet to n
// listeners on UDP: the whole cost on the sender's goroutine, with one
// encrypt and one sendto per listener. deliver ignores send errors (macOS
// loopback can give ENOBUFS under load), so the readers count what arrived
// and the benchmark fails if too little did. A datagram can also be lost at
// the receiver's buffer, so "delivered" is a lower bound on the sends that
// worked.
func BenchmarkVoiceToUDPListeners(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			s, speaker, received := udpListeners(b, n)
			first := received.Load() // the packets of the first-packet check
			packet := audio(1, make([]byte, 40))
			b.ReportAllocs()
			for b.Loop() {
				s.voice(speaker, packet, false)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/receiver")
			// Outside the timer: wait until the readers have drained.
			sent := int64(b.N) * int64(n)
			last := int64(-1)
			for wait := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
				got := received.Load() - first
				if got >= sent || got == last || time.Now().After(wait) {
					break
				}
				last = got
			}
			got := received.Load() - first
			b.ReportMetric(float64(got)/float64(sent), "delivered")
			if got*10 < sent*9 {
				b.Fatalf("only %d of %d datagrams arrived", got, sent)
			}
		})
	}
}

// BenchmarkVoiceToUDPListenersNoSend is the same without the sendto: the
// encode and one encrypt per listener. (For the listeners on TCP queues
// instead, see BenchmarkVoiceToChannel.) It opens no socket.
func BenchmarkVoiceToUDPListenersNoSend(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("listeners=%d", n), func(b *testing.B) {
			s, _ := voiceListeners(n)
			p, _ := decodeAudio(audio(1, make([]byte, 40)), false)
			b.ReportAllocs()
			for b.Loop() {
				out := encodeServerAudio(p, 1, ContextNormal, false, false)
				for session := uint32(2); session <= uint32(n)+1; session++ {
					c := s.clients[session]
					c.cryptMu.Lock()
					c.crypt.Encrypt(out)
					c.cryptMu.Unlock()
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/receiver")
		})
	}
}
