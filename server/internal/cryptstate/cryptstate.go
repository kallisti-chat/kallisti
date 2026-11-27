package cryptstate

// CryptState is one side of a UDP connection: packets have a 4-byte header
// (the IV's low byte and the first 3 tag bytes), and each direction has a
// 16-byte IV that counts up little-endian. Decrypt accepts packets that are
// lost or up to 30 late, and refuses replays. Not safe for concurrent use.
type CryptState struct {
	// One per direction, each with its own block buffer, so that Encrypt
	// and Decrypt still share no state.
	encrypter, decrypter *aesBlock
	EncryptIV            [16]byte
	DecryptIV            [16]byte
	// Per low IV byte: the second IV byte of the last packet seen with it.
	history [256]byte

	Good, Late, Lost uint32
}

// New makes a CryptState. On the server, the client nonce decrypts and the
// server nonce encrypts.
func New(key, encryptIV, decryptIV [16]byte) *CryptState {
	return &CryptState{encrypter: newCipher(key), decrypter: newCipher(key), EncryptIV: encryptIV, DecryptIV: decryptIV}
}

func increment(iv *[16]byte, from int) {
	for i := from; i < 16; i++ {
		iv[i]++
		if iv[i] != 0 {
			return
		}
	}
}

func decrement(iv *[16]byte, from int) {
	for i := from; i < 16; i++ {
		iv[i]--
		if iv[i] != 0xff {
			return
		}
	}
}

func (cs *CryptState) Encrypt(plain []byte) []byte {
	increment(&cs.EncryptIV, 0)
	packet, tag := ocbEncrypt(make([]byte, 4, 4+len(plain)), cs.encrypter, cs.EncryptIV, plain)
	packet[0] = cs.EncryptIV[0]
	copy(packet[1:4], tag[:3])
	return packet
}

func (cs *CryptState) Decrypt(packet []byte) ([]byte, bool) {
	if len(packet) < 4 {
		return nil, false
	}
	saved := cs.DecryptIV
	ivbyte, current := packet[0], cs.DecryptIV[0]
	restore := false
	late, lost := 0, 0
	if current+1 == ivbyte {
		// In order.
		if ivbyte < current {
			increment(&cs.DecryptIV, 1)
		}
		cs.DecryptIV[0] = ivbyte
	} else {
		diff := int(ivbyte) - int(current)
		if diff > 128 {
			diff -= 256
		} else if diff < -128 {
			diff += 256
		}
		switch {
		case ivbyte < current && diff > -30 && diff < 0:
			// Late, no wrap-around.
			late, lost, restore = 1, -1, true
			cs.DecryptIV[0] = ivbyte
		case ivbyte > current && diff > -30 && diff < 0:
			// Late, from before a wrap-around.
			late, lost, restore = 1, -1, true
			cs.DecryptIV[0] = ivbyte
			decrement(&cs.DecryptIV, 1)
		case ivbyte > current && diff > 0:
			// Some packets lost.
			lost = diff - 1
			cs.DecryptIV[0] = ivbyte
		case ivbyte < current && diff > 0:
			// Some packets lost, with a wrap-around.
			lost = diff - 1
			cs.DecryptIV[0] = ivbyte
			increment(&cs.DecryptIV, 1)
		default:
			return nil, false
		}
		if cs.history[cs.DecryptIV[0]] == cs.DecryptIV[1] {
			cs.DecryptIV = saved
			return nil, false // replay
		}
	}
	plain, tag, ok := ocbDecrypt(cs.decrypter, cs.DecryptIV, packet[4:])
	if !ok || tag[0] != packet[1] || tag[1] != packet[2] || tag[2] != packet[3] {
		cs.DecryptIV = saved
		return nil, false
	}
	cs.history[cs.DecryptIV[0]] = cs.DecryptIV[1]
	if restore {
		cs.DecryptIV = saved
	}
	cs.Good++
	cs.Late += uint32(late)
	if lost > 0 || cs.Lost > 0 {
		cs.Lost = uint32(int(cs.Lost) + lost)
	}
	return plain, true
}
