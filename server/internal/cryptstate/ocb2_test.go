package cryptstate

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Test vectors from draft-krovetz-ocb-00.txt, as in Mumble's TestCrypt.
var vectorKey = [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestEmptyMessageVector(t *testing.T) {
	crypted, tag := ocbEncrypt(nil, newCipher(vectorKey), vectorKey, nil)
	if len(crypted) != 0 || !bytes.Equal(tag[:], mustHex("BF3108130773AD5EC70EC69E7875A7B0")) {
		t.Fatalf("got %x %x", crypted, tag)
	}
}

func TestFortyByteVector(t *testing.T) {
	plain := make([]byte, 40)
	for i := range plain {
		plain[i] = byte(i)
	}
	crypted, tag := ocbEncrypt(nil, newCipher(vectorKey), vectorKey, plain)
	if !bytes.Equal(tag[:], mustHex("9DB0CDF880F73E3E10D4EB3217766688")) {
		t.Fatalf("tag %x", tag)
	}
	want := mustHex("F75D6BC8B4DC8D66B836A2B08B32A6369F1CD3C5228D79FD6C267F5F6AA7B231C7DFB9D59951AE9C")
	if !bytes.Equal(crypted, want) {
		t.Fatalf("crypted %x", crypted)
	}
}

func TestSameOutputAsTheDesktopBridge(t *testing.T) {
	// Outputs of apps/desktop/src-tauri/src/ocb2.rs for the same input.
	key, nonce := [16]byte{}, [16]byte{}
	for i := range key {
		key[i], nonce[i] = 3, 1
	}
	plain := make([]byte, 40)
	for i := 2; i < 40; i++ {
		plain[i] = byte(0x41 + i - 2)
	}
	for _, c := range []struct {
		plain        []byte
		crypted, tag string
	}{
		{[]byte{0x20, 1}, "8d1b", "191b2d"},
		{plain, "1c6aab638e57bb35e0603afb75d5b725924e471a0181e483a746ed01bf0e8ea6b4747fbd60cba989", "58655a"},
	} {
		crypted, tag := ocbEncrypt(nil, newCipher(key), nonce, c.plain)
		if hex.EncodeToString(crypted) != c.crypted || hex.EncodeToString(tag[:3]) != c.tag {
			t.Errorf("got %x %x", crypted, tag[:3])
		}
	}
}

func TestRoundTripsAllLengths(t *testing.T) {
	nonce := [16]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00}
	for n := 0; n < 128; n++ {
		plain := make([]byte, n)
		for i := range plain {
			plain[i] = byte(i*7 + 3)
		}
		crypted, tag := ocbEncrypt(nil, newCipher(vectorKey), nonce, plain)
		decrypted, dectag, ok := ocbDecrypt(newCipher(vectorKey), nonce, crypted)
		if !ok || !bytes.Equal(decrypted, plain) || dectag != tag {
			t.Fatalf("length %d", n)
		}
	}
}

func TestRefusesTheXEXStarAttackPattern(t *testing.T) {
	nonce := [16]byte{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}
	plain := make([]byte, 32)
	plain[15] = 128
	crypted, tag := ocbEncrypt(nil, newCipher(vectorKey), nonce, plain)
	decrypted, dectag, ok := ocbDecrypt(newCipher(vectorKey), nonce, crypted)
	if !ok || bytes.Equal(decrypted, plain) || dectag != tag {
		t.Fatal("the critical block must be changed, and still decrypt")
	}
}
