package mumbled

import (
	"bytes"
	"maps"
	"net"
	"net/netip"
	"slices"
	"time"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/recording"
)

// voice relays an audio packet from c (over TCP or UDP) to its receivers.
func (s *Server) voice(c *client, packet []byte, tcp bool) {
	p, ok := decodeAudio(packet, c.legacy)
	if !ok {
		return
	}
	// As Murmur: headers count too (IP, UDP, crypt).
	now, size := time.Now(), 20+8+4+len(packet)
	c.stats.voice(p, tcp, size, now)
	if !c.bandwidth.AllowN(now, size) {
		return
	}
	s.mu.Lock()
	var receivers []receiver
	var rec *recording.Recording
	var speaker string
	sender := s.state.Users[c.session]
	if sender != nil && s.recordings != nil {
		rec, speaker = s.recorderForLocked(sender, p.target), sender.Name
	}
	for session, ctx := range s.state.Receivers(c.session, p.target) {
		o := s.clients[session]
		if o == nil || sender == nil {
			continue
		}
		// As Murmur: the position only within the same game.
		r := receiver{c: o, ctx: ctx, position: bytes.Equal(sender.PluginContext, s.state.Users[session].PluginContext)}
		if o.udpVoice && s.udp != nil {
			r.addr = o.udpAddr
		}
		receivers = append(receivers, r)
	}
	udp := s.udp
	s.mu.Unlock()
	deliver(p, c.session, receivers, udp)
	if rec != nil {
		rec.Feed(recording.Packet{Speaker: speaker, Seq: p.frame, Opus: p.opus, Terminator: p.terminator, At: now})
	}
}

// receiver is one client a voice packet goes to.
type receiver struct {
	c        *client
	addr     netip.AddrPort // UDP, if it uses it
	ctx      AudioContext
	position bool
}

// deliver sends a voice packet from a session to its receivers (outside
// the lock): one packet (and TCP frame) per format and context, at most
// eight.
func deliver(p audioPacket, from uint32, receivers []receiver, udp *net.UDPConn) {
	type kind struct {
		legacy, position bool
		ctx              AudioContext
	}
	type packetFor struct{ udp, tcp []byte }
	packets := map[kind]packetFor{}
	for _, r := range receivers {
		k := kind{r.c.legacy, r.position, r.ctx}
		out, ok := packets[k]
		if !ok {
			out.udp = encodeServerAudio(p, from, r.ctx, r.c.legacy, r.position)
			out.tcp = mumble.EncodeFrame(mumble.TypeUDPTunnel, out.udp)
			packets[k] = out
		}
		if r.addr.IsValid() {
			r.c.cryptMu.Lock()
			crypted := r.c.crypt.Encrypt(out.udp)
			r.c.cryptMu.Unlock()
			udp.WriteToUDPAddrPort(crypted, r.addr)
		} else {
			r.c.send(out.tcp, true)
		}
	}
}

// ServeUDP handles UDP voice and pings until the socket closes.
func (s *Server) ServeUDP(conn *net.UDPConn) error {
	s.mu.Lock()
	s.udp = conn
	s.mu.Unlock()
	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return err
		}
		s.udpPacket(conn, addr, buf[:n])
	}
}

// decrypt tries a client's key. If its packets stopped decrypting, it
// returns true for requestSync (as Murmur's checkDecrypt).
func (c *client) decrypt(packet []byte, now time.Time) (plain []byte, ok, requestSync bool) {
	c.cryptMu.Lock()
	defer c.cryptMu.Unlock()
	if plain, ok = c.crypt.Decrypt(packet); ok {
		c.lastGood = now
		return plain, true, false
	}
	if now.Sub(c.lastGood) > resyncAfter && now.Sub(c.lastRequest) > resyncAfter {
		c.lastRequest = now
		return nil, false, true
	}
	return nil, false, false
}

func (s *Server) udpPacket(conn *net.UDPConn, addr netip.AddrPort, packet []byte) {
	addr = netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
	// A known address, or else: try the clients from the same IP, as
	// Murmur does.
	// Server-list pings come unencrypted, also from known addresses (as
	// Murmur); voice never looks like one in practice.
	if s.serverPing(conn, addr, packet) {
		return
	}
	s.mu.Lock()
	known := s.byAddr[addr]
	candidates := []*client{known}
	if known == nil {
		// Clients stay in byHost after their address is known, so a new
		// address after a NAT rebinding still finds them.
		candidates = slices.Collect(maps.Keys(s.byHost[addr.Addr()]))
	}
	s.mu.Unlock()
	now := time.Now()
	for _, c := range candidates {
		plain, ok, requestSync := c.decrypt(packet, now)
		if requestSync && c == known {
			c.sendMsg(mumble.TypeCryptSetup, &pb.CryptSetup{})
		}
		if !ok {
			continue
		}
		s.mu.Lock()
		if c.udpAddr != addr {
			if s.byAddr[c.udpAddr] == c {
				delete(s.byAddr, c.udpAddr)
			}
			c.udpAddr = addr
			s.byAddr[addr] = c
		}
		// As Murmur: only voice (not pings) over UDP means the client
		// gets voice over UDP. Also a voice packet that s.voice drops: the
		// apps' bridge sends an empty one once its UDP works both ways.
		kind := udpKind(plain, c.legacy)
		isAudio := kind == udpAudio
		if isAudio {
			c.udpVoice = true
		}
		s.mu.Unlock()
		if isAudio {
			s.voice(c, plain, false)
		} else if kind == udpPing {
			// Pings come back as they are.
			c.cryptMu.Lock()
			pong := c.crypt.Encrypt(plain)
			c.cryptMu.Unlock()
			conn.WriteToUDPAddrPort(pong, addr)
		}
		return
	}
}

const (
	udpOther = iota
	udpAudio
	udpPing
)

// udpKind tells audio and pings apart in the client's format.
func udpKind(plain []byte, legacy bool) int {
	switch {
	case len(plain) == 0:
		return udpOther
	case legacy && plain[0]>>5 == legacyOpus, !legacy && plain[0] == 0:
		return udpAudio
	case legacy && plain[0]>>5 == 1, !legacy && plain[0] == 1:
		return udpPing
	}
	return udpOther
}
