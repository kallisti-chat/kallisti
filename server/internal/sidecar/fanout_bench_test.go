package sidecar

import (
	"fmt"
	"testing"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// readyConns connects n ready users to a hub with one channel.
func readyConns(tb testing.TB, n int) (*Hub, []*Conn) {
	tb.Helper()
	h := newHub(tb, "general")
	conns := make([]*Conn, n)
	for i := range conns {
		c := h.Connect()
		tb.Cleanup(c.Close)
		c.SetIdentity(mumble.Identity{Session: uint32(i + 1), Name: fmt.Sprintf("user%d", i)})
		<-c.Outbox() // ready
		conns[i] = c
	}
	return h, conns
}

// BenchmarkFanOut sends one message event to n connections.
func BenchmarkFanOut(b *testing.B) {
	m := store.Message{ID: 1, ChannelID: 1, Author: "user0", Text: "hello everyone, this is a typical chat message", CreatedAt: clock}
	for _, n := range []int{50, 500} {
		h, conns := readyConns(b, n)
		ch, _ := h.chatOf(1)
		ev := func() map[string]any { return map[string]any{"type": "message", "message": h.toWire(m)} }
		run := func(name string, f func()) {
			b.Run(fmt.Sprintf("%s/%d", name, n), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					f()
					drain(conns...)
				}
			})
		}
		run("broadcast", func() { h.broadcast(ev()) })
		run("sendTo", func() { h.sendTo(ch, ev()) })
		run("deliver", func() { h.deliver(mumble.Identity{Session: 1, Name: "user0"}, ch, m, conns[0], "n1") })
	}
}
