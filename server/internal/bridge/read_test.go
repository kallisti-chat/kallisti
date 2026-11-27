package bridge

import (
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/coder/websocket"
)

func TestBinaryMessagesArriveOneByOne(t *testing.T) {
	far := make(chan net.Conn, 1)
	url := startBridge(t, Options{Dial: pipeDialer(far)})
	c, _, err := connect(t, url, "")
	if err != nil {
		t.Fatal(err)
	}
	up := <-far
	var want [][]byte
	// Different sizes, so each read reuses a buffer the last one left.
	for i, n := range []int{100, 7, 1, 300, 100, 2000, 5, 100} {
		m := bytes.Repeat([]byte{byte(i + 1)}, n)
		want = append(want, m)
		if err := c.Write(testCtx(t), websocket.MessageBinary, m); err != nil {
			t.Fatal(err)
		}
	}
	for i, m := range want {
		got := make([]byte, len(m))
		if _, err := io.ReadFull(up, got); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if !bytes.Equal(got, m) {
			t.Fatalf("message %d differs", i)
		}
	}
}

func TestALargeMessageDoesNotStayAllocated(t *testing.T) {
	var buf []byte
	big := bytes.Repeat([]byte{9}, 1<<20)
	got, buf, err := readBinary(bytes.NewReader(big), buf)
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("big message: %d bytes, %v", len(got), err)
	}
	if cap(buf) != 0 {
		t.Errorf("kept a buffer of %d bytes after a large message", cap(buf))
	}
	small := []byte{1, 2, 3}
	got, buf, err = readBinary(bytes.NewReader(small), buf)
	if err != nil || !bytes.Equal(got, small) {
		t.Fatalf("small message: %v, %v", got, err)
	}
	if cap(buf) == 0 || cap(buf) > maxKeptBuffer {
		t.Errorf("buffer of %d bytes, want one up to %d", cap(buf), maxKeptBuffer)
	}
	// The buffer is reused for the next message.
	first := &buf[:1][0]
	got, _, _ = readBinary(bytes.NewReader([]byte{4, 5}), buf)
	if &got[0] != first {
		t.Error("the next message did not reuse the buffer")
	}
}

func TestALargeMessageThenSmallOnes(t *testing.T) {
	far := make(chan net.Conn, 1)
	url := startBridge(t, Options{Dial: pipeDialer(far)})
	c, _, err := connect(t, url, "")
	if err != nil {
		t.Fatal(err)
	}
	up := <-far
	msgs := [][]byte{bytes.Repeat([]byte{1}, 1<<20), {2, 2}, bytes.Repeat([]byte{3}, 100)}
	go func() {
		for _, m := range msgs {
			c.Write(testCtx(t), websocket.MessageBinary, m)
		}
	}()
	for i, m := range msgs {
		got := make([]byte, len(m))
		if _, err := io.ReadFull(up, got); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if !bytes.Equal(got, m) {
			t.Fatalf("message %d differs", i)
		}
	}
}
