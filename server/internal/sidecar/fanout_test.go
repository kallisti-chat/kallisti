package sidecar

import (
	"bytes"
	"encoding/json"
	"testing"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Every reader of an event gets the same bytes that send would queue.
func TestFanOutQueuesTheSameBytesForEveryReader(t *testing.T) {
	h, conns := readyConns(t, 5)
	ch, _ := h.chatOf(1)
	m := store.Message{ID: 7, ChannelID: 1, Author: "user0", Text: "hi", CreatedAt: clock}
	ev := map[string]any{"type": "message", "message": h.toWire(m)}
	want, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, skip *Conn) {
		t.Helper()
		for _, c := range conns {
			if c == skip {
				continue
			}
			select {
			case got := <-c.Outbox():
				if !bytes.Equal(got, want) || !json.Valid(got) {
					t.Errorf("%s: got %s, want %s", name, got, want)
				}
			default:
				t.Errorf("%s: nothing queued", name)
			}
		}
		drain(conns...)
	}
	h.broadcast(ev)
	check("broadcast", nil)
	h.sendTo(ch, ev)
	check("sendTo", nil)
	// The sender's own copy carries its nonce (see also TestSendWithANonce).
	h.deliver(mumble.Identity{Session: 1, Name: "user0"}, ch, m, conns[0], "n1")
	var mine map[string]any
	if err := json.Unmarshal(<-conns[0].Outbox(), &mine); err != nil || mine["nonce"] != "n1" {
		t.Errorf("deliver: sender got %v, %v", mine, err)
	}
	check("deliver", conns[0])
}
