package bridge

import (
	"context"
	"net"
	"testing"

	"github.com/coder/websocket"
)

// pipeDialer gives the bridge one end of a net.Pipe (the built-in upstream
// is one too) and hands the other end to the test.
func pipeDialer(far chan<- net.Conn) func(context.Context, Upstream) (net.Conn, error) {
	return func(context.Context, Upstream) (net.Conn, error) {
		near, f := net.Pipe()
		far <- f
		return near, nil
	}
}

// BenchmarkClientToUpstream reads 100-byte binary messages (the size of a
// voice packet) through the real read loop.
func BenchmarkClientToUpstream(b *testing.B) {
	far := make(chan net.Conn, 1)
	url := startBridge(b, Options{Dial: pipeDialer(far)})
	c, _, err := connect(b, url, "")
	if err != nil {
		b.Fatal(err)
	}
	up := <-far
	msg := make([]byte, 100)
	done := make(chan struct{})
	go func() {
		defer close(done)
		got := 0
		buf := make([]byte, 4096)
		for got < b.N*len(msg) {
			n, err := up.Read(buf)
			if err != nil {
				return
			}
			got += n
		}
	}()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Write(ctx, websocket.MessageBinary, msg); err != nil {
			b.Fatal(err)
		}
	}
	<-done
	b.StopTimer()
}
