package sidecar

import (
	"fmt"
	"testing"

	"kallisti.chat/server/internal/push"
	"kallisti.chat/server/internal/store"
)

// BenchmarkPushFor decides the pushes for a message with n subscribed
// accounts (every tenth with #general on "All messages"), none connected.
// pushusers/op: PushUser calls (two database lookups each in the server).
func BenchmarkPushFor(b *testing.B) {
	for _, n := range []int{50, 500} {
		users, all := map[string]uint32{}, map[string]bool{}
		for i := range n {
			name := fmt.Sprintf("user%03d", i)
			users[name] = uint32(1000 + i)
			all[name] = i%10 == 0
		}
		h, v, _, calls := pushUsersHub(b, users, all)
		v.online = nil
		sent := 0
		h.Push = func(string, push.Note) { sent++ } // not asking again: that counts as PushUser calls
		direct, err := h.store.DirectChat(1000, 1001)
		if err != nil {
			b.Fatal(err)
		}
		from := func(text string) store.Message {
			return store.Message{ID: 1, Author: "user000", Verified: true, AuthorID: new(uint32(1000)), Text: text, CreatedAt: clock}
		}
		general := chat{id: h.channels[0].ID}
		for _, c := range []struct {
			name string
			c    chat
			m    store.Message
		}{
			{"plain", general, from("is anyone around for lunch today?")},
			{"mention", general, from("@user007 are you around for lunch today?")},
			{"direct", chat{id: direct, direct: true, peers: [2]uint32{1000, 1001}}, from("are you around for lunch today?")},
		} {
			b.Run(fmt.Sprintf("%s/%d", c.name, n), func(b *testing.B) {
				b.ReportAllocs()
				calls.Store(0)
				ops := 0
				sent = 0
				for b.Loop() {
					h.pushFor(c.c, c.m)
					ops++
				}
				b.ReportMetric(float64(calls.Load())/float64(ops), "pushusers/op")
				b.ReportMetric(float64(sent)/float64(ops), "pushes/op")
			})
		}
	}
}
