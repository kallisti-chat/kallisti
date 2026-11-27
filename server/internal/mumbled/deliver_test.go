package mumbled

import (
	"bytes"
	"net"
	"testing"
	"time"

	"kallisti.chat/server/internal/cryptstate"
	"kallisti.chat/server/internal/mumble"
)

func TestDeliverToUDPAndTCPReceivers(t *testing.T) {
	sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	addr := sock.LocalAddr().(*net.UDPAddr).AddrPort()

	key, serverIV, clientIV := [16]byte{1}, [16]byte{2}, [16]byte{3}
	p := audioPacket{frame: 7, opus: []byte{0xfc, 1, 2, 3}, positional: []float32{1, 2, 3}}

	type recv struct {
		name             string
		udp              bool
		legacy, position bool
		ctx              AudioContext
	}
	recvs := []recv{
		{"udp", true, false, false, ContextNormal},
		{"tcp same kind as the udp one", false, false, false, ContextNormal},
		{"tcp", false, false, false, ContextNormal},
		{"udp position", true, false, true, ContextNormal},
		{"tcp position", false, false, true, ContextNormal},
		{"tcp only kind", false, false, false, ContextShout},
		{"udp only kind", true, false, false, ContextWhisper},
		{"legacy udp", true, true, false, ContextNormal},
		{"legacy tcp", false, true, false, ContextNormal},
		{"legacy tcp position", false, true, true, ContextListen},
		{"legacy udp position", true, true, true, ContextListen},
	}
	clients := make([]*client, len(recvs))
	var receivers []receiver
	for i, r := range recvs {
		c := &client{
			legacy: r.legacy,
			voice:  make(chan []byte, 4),
			crypt:  cryptstate.New(key, serverIV, clientIV),
		}
		clients[i] = c
		rc := receiver{c: c, ctx: r.ctx, position: r.position}
		if r.udp {
			rc.addr = addr
		}
		receivers = append(receivers, rc)
	}
	deliver(p, 42, receivers, sender)

	// What each kind of client's packet is, as encodeServerAudio makes it.
	buf := make([]byte, 2048)
	sock.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i, r := range recvs {
		want := encodeServerAudio(p, 42, r.ctx, r.legacy, r.position)
		c := clients[i]
		if r.udp {
			n, _, err := sock.ReadFromUDPAddrPort(buf)
			if err != nil {
				t.Fatalf("%s: %v", r.name, err)
			}
			// Each client has its own crypt state, so its own peer.
			got, ok := cryptstate.New(key, clientIV, serverIV).Decrypt(buf[:n])
			if !ok || !bytes.Equal(got, want) {
				t.Errorf("%s: udp payload %x, want %x", r.name, got, want)
			}
			if len(c.voice) != 0 {
				t.Errorf("%s: also got a tcp frame", r.name)
			}
			continue
		}
		select {
		case got := <-c.voice:
			if !bytes.Equal(got, mumble.EncodeFrame(mumble.TypeUDPTunnel, want)) {
				t.Errorf("%s: tcp frame %x, want the framed %x", r.name, got, want)
			}
			if len(got) != cap(got) {
				t.Errorf("%s: frame len %d, cap %d: a later append could write into shared memory", r.name, len(got), cap(got))
			}
		default:
			t.Errorf("%s: no tcp frame", r.name)
		}
	}
}
