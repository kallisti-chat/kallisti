package cryptstate

import (
	"fmt"
	"testing"
)

// An Opus voice packet is about 80 bytes; the larger size is near the MTU.
var benchSizes = []int{80, 1020}

func benchPlain(n int) []byte {
	plain := make([]byte, n)
	for i := range plain {
		plain[i] = byte(i*7 + 3)
	}
	return plain
}

func BenchmarkEncrypt(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("bytes=%d", n), func(b *testing.B) {
			client, _ := pair()
			plain := benchPlain(n)
			b.SetBytes(int64(n))
			b.ReportAllocs()
			for b.Loop() {
				client.Encrypt(plain)
			}
		})
	}
}

func BenchmarkDecrypt(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("bytes=%d", n), func(b *testing.B) {
			client, server := pair()
			plain := benchPlain(n)
			// Packets in order, then the decrypt IV goes back to the start:
			// in-order packets skip the replay check, so all of them decrypt.
			packets := make([][]byte, 200)
			for i := range packets {
				packets[i] = client.Encrypt(plain)
			}
			start := server.DecryptIV
			b.SetBytes(int64(n))
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				if i == len(packets) {
					i, server.DecryptIV = 0, start
				}
				if _, ok := server.Decrypt(packets[i]); !ok {
					b.Fatal("packet did not decrypt")
				}
				i++
			}
		})
	}
}
