package mumbled

import (
	"fmt"
	"testing"

	"golang.org/x/time/rate"
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
