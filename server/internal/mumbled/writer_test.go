package mumbled

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"kallisti.chat/server/internal/certs"
)

// keepOpen lets one connection serve many runs of the writer, which
// closes it at the end of each.
type keepOpen struct{ net.Conn }

func (keepOpen) Close() error { return nil }

// tlsPair is a TLS connection over loopback, as native clients have.
func tlsPair(b *testing.B) (server, client net.Conn) {
	certPEM, keyPEM, err := certs.GenerateServer("bench")
	if err != nil {
		b.Fatal(err)
	}
	cert, err := certs.Load(certPEM, keyPEM)
	if err != nil {
		b.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		s := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}})
		s.Handshake()
		accepted <- s
	}()
	client, err = tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		b.Fatal(err)
	}
	server = <-accepted
	b.Cleanup(func() { server.Close(); client.Close() })
	return server, client
}

func pipePair(b *testing.B) (server, client net.Conn) {
	server, client = net.Pipe()
	b.Cleanup(func() { server.Close(); client.Close() })
	return server, client
}

// The writer draining voice frames that are already queued, as when it
// fell behind.
func BenchmarkWriterQueuedVoice(b *testing.B) {
	frame := make([]byte, 70) // a small Opus packet with its headers
	for _, path := range []struct {
		name string
		pair func(*testing.B) (net.Conn, net.Conn)
	}{{"tls", tlsPair}, {"pipe", pipePair}} {
		for _, n := range []int{1, 4, 32} {
			b.Run(fmt.Sprintf("%s/frames=%d", path.name, n), func(b *testing.B) {
				srv, peer := path.pair(b)
				c := &client{conn: keepOpen{srv}, out: make(chan []byte, 1), voice: make(chan []byte, n)}
				got := make([]byte, n*len(frame))
				read := make(chan error)
				go func() {
					for {
						_, err := io.ReadFull(peer, got)
						read <- err
						if err != nil {
							return
						}
					}
				}()
				b.ReportAllocs()
				b.SetBytes(int64(len(got)))
				b.ResetTimer()
				for range b.N {
					for range n {
						c.send(frame, true)
					}
					c.closeAfterFlush()
					c.writer()
					if err := <-read; err != nil {
						b.Fatal(err)
					}
					c.closeOnce = sync.Once{}
				}
			})
		}
	}
}
