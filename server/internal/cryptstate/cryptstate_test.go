package cryptstate

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

func pair() (client, server *CryptState) {
	key := [16]byte{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}
	clientNonce := [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	serverNonce := [16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	return New(key, clientNonce, serverNonce), New(key, serverNonce, clientNonce)
}

func TestPacketsRoundTripInOrder(t *testing.T) {
	client, server := pair()
	for i := 0; i < 600; i++ {
		msg := []byte(fmt.Sprintf("voice packet %d", i))
		packet := client.Encrypt(msg)
		if len(packet) != len(msg)+4 {
			t.Fatalf("length %d", len(packet))
		}
		if got, ok := server.Decrypt(packet); !ok || !bytes.Equal(got, msg) {
			t.Fatalf("packet %d", i)
		}
	}
}

func TestLostAndLatePacketsStillDecrypt(t *testing.T) {
	client, server := pair()
	// Not all zeros: OCB2's countermeasure changes such blocks on purpose.
	data := func(i int) []byte { return bytes.Repeat([]byte{byte(0x41 + i)}, 20) }
	var packets [][]byte
	for i := 0; i < 10; i++ {
		packets = append(packets, client.Encrypt(data(i)))
	}
	for _, i := range []int{0, 3, 2, 4} { // 1 is lost, 2 is late
		if got, ok := server.Decrypt(packets[i]); !ok || !bytes.Equal(got, data(i)) {
			t.Fatalf("packet %d", i)
		}
	}
	if server.Good != 4 || server.Late != 1 {
		t.Fatalf("good %d late %d", server.Good, server.Late)
	}
}

func TestReplayedAndTamperedPacketsAreRefused(t *testing.T) {
	client, server := pair()
	first, second := client.Encrypt([]byte("hello")), client.Encrypt([]byte("world"))
	server.Decrypt(first)
	server.Decrypt(second)
	if _, ok := server.Decrypt(first); ok {
		t.Fatal("replay accepted")
	}
	bad := client.Encrypt([]byte("tampered"))
	bad[len(bad)-1] ^= 1
	if _, ok := server.Decrypt(bad); ok {
		t.Fatal("tampered packet accepted")
	}
	if _, ok := server.Decrypt([]byte{1, 2}); ok {
		t.Fatal("short packet accepted")
	}
}

func TestAWrongKeyDoesNotDecrypt(t *testing.T) {
	client, _ := pair()
	other := New([16]byte{4}, [16]byte{2}, [16]byte{1})
	if _, ok := other.Decrypt(client.Encrypt([]byte("secret"))); ok {
		t.Fatal("decrypted with the wrong key")
	}
}

func TestAllocationsPerPacket(t *testing.T) {
	client, server := pair()
	plain := bytes.Repeat([]byte{0x41}, 80)
	packet := client.Encrypt(plain)
	// One allocation each: the packet and the plaintext, which callers keep.
	if n := testing.AllocsPerRun(100, func() { client.Encrypt(plain) }); n != 1 {
		t.Errorf("Encrypt: %v allocations", n)
	}
	start := server.DecryptIV
	if n := testing.AllocsPerRun(100, func() {
		server.DecryptIV = start
		if _, ok := server.Decrypt(packet); !ok {
			t.Fatal("packet did not decrypt")
		}
	}); n != 1 {
		t.Errorf("Decrypt: %v allocations", n)
	}
	// None inside OCB2 itself.
	c, dst := newCipher(vectorKey), make([]byte, 0, len(plain))
	if n := testing.AllocsPerRun(100, func() { ocbEncrypt(dst, c, vectorKey, plain) }); n != 0 {
		t.Errorf("ocbEncrypt: %v allocations", n)
	}
}

// Two sides talking both ways from several goroutines, each side behind one
// mutex as the server keeps it (client.cryptMu). Run with -race: block
// buffers shared between states would show up here.
func TestConcurrentSidesRoundTrip(t *testing.T) {
	const senders, perSender = 4, 200
	type side struct {
		mu sync.Mutex
		cs *CryptState
		in chan []byte
	}
	client, server := pair()
	// Room for every packet: a sender holds its lock while it sends.
	a := &side{cs: client, in: make(chan []byte, senders*perSender)}
	b := &side{cs: server, in: make(chan []byte, senders*perSender)}
	msg := func(from, i int) []byte {
		m := make([]byte, 80)
		for j := range m {
			m[j] = byte(from*31 + i*7 + j)
		}
		return m
	}
	var wg sync.WaitGroup
	for _, dir := range []struct{ from, to *side }{{a, b}, {b, a}} {
		for s := range senders {
			wg.Go(func() {
				for i := range perSender {
					// Sent under the lock, so packets arrive in IV order.
					dir.from.mu.Lock()
					dir.to.in <- append([]byte{byte(s)}, dir.from.cs.Encrypt(msg(s, i))...)
					dir.from.mu.Unlock()
				}
			})
		}
		wg.Go(func() {
			next := make([]int, senders)
			for range senders * perSender {
				p := <-dir.to.in
				s := int(p[0])
				dir.to.mu.Lock()
				got, ok := dir.to.cs.Decrypt(p[1:])
				dir.to.mu.Unlock()
				if !ok || !bytes.Equal(got, msg(s, next[s])) {
					t.Errorf("sender %d packet %d did not round-trip", s, next[s])
					return
				}
				next[s]++
			}
		})
	}
	wg.Wait()
}
