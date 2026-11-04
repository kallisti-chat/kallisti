package mumbled

import (
	"cmp"
	"net/netip"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// Connection stats: what the server measures per connection, for UserStats
// (Mumble's "User Information") and ConnectionStats. Cheap: one small lock
// per voice packet, no allocation.

// VoiceStats counts the voice packets of one direction.
type VoiceStats struct{ Good, Late, Lost, Resync uint32 }

// Ping is a round trip time in milliseconds, as the client measured it.
type Ping struct{ Avg, Var float32 }

// Reported is what the client measured, from its Ping messages.
type Reported struct {
	FromServer             VoiceStats // UDP from the server
	TCPPing, UDPPing       Ping
	TCPPackets, UDPPackets uint32 // pings measured
}

// ConnStats is what the server knows about one connection (see
// ConnectionStats).
type ConnStats struct {
	Session   uint32
	Name      string
	Transport string // of its voice: "udp", "tcp", "websocket" (the web bridge) or "websocket+udp" (the apps' bridge)
	Address   netip.Addr
	// The client's version (Mumble's version_v2) and system, as it said.
	Version                uint64
	Release, OS, OSVersion string
	Online, Idle           time.Duration // idle: no voice or text
	// Voice from the client: over UDP (the crypt counts it) and over TCP
	// (counted by frame numbers).
	UDP, TCP VoiceStats
	Reported
	// Its voice in bytes per second (with headers, as Murmur), also what
	// the bandwidth limit dropped.
	Bandwidth     uint32
	Dropped       uint64 // voice frames it did not get: its queue was full
	TextOverflows uint64 // sidecar messages it lost (see Config.TextOverflows)
	Codec         Codec  // its voice lately (see codecstats.go)
}

// ConnectionStats is the stats of every connection, by session.
func (s *Server) ConnectionStats() []ConnStats {
	type conn struct {
		c    *client
		name string
		udp  bool
	}
	s.mu.Lock()
	conns := make([]conn, 0, len(s.clients))
	for session, c := range s.clients {
		if u := s.state.Users[session]; u != nil {
			conns = append(conns, conn{c, u.Name, c.udpVoice})
		}
	}
	s.mu.Unlock()
	var overflows map[uint32]uint64
	if s.cfg.TextOverflows != nil {
		overflows = s.cfg.TextOverflows()
	}
	now := time.Now()
	out := make([]ConnStats, len(conns))
	for i, x := range conns {
		out[i] = x.c.connStats(x.name, x.udp, now)
		out[i].TextOverflows = overflows[x.c.session]
	}
	slices.SortFunc(out, func(a, b ConnStats) int { return cmp.Compare(a.Session, b.Session) })
	return out
}

// connStats: name and udpVoice are read under Server.mu.
func (c *client) connStats(name string, udpVoice bool, now time.Time) ConnStats {
	st := ConnStats{
		Session: c.session, Name: name, Transport: "tcp", Address: c.host,
		Version: c.version, Release: c.release, OS: c.os, OSVersion: c.osVersion,
		UDP: c.cryptStats(), Dropped: c.dropped.Load(),
	}
	if c.web && udpVoice {
		st.Transport = "websocket+udp"
	} else if c.web {
		st.Transport = "websocket"
	} else if udpVoice {
		st.Transport = "udp"
	}
	m := &c.stats
	m.mu.Lock()
	defer m.mu.Unlock()
	st.Online, st.Idle = now.Sub(m.since), now.Sub(m.active)
	st.TCP = VoiceStats{Good: m.tcp.good, Late: m.tcp.late, Lost: m.tcp.lost}
	st.Reported = m.reported
	st.Bandwidth = m.bandwidth(now)
	st.Codec = m.recent.codec()
	return st
}

// connStats is what a client's connection measured, beside the crypt.
type connStats struct {
	mu       sync.Mutex
	since    time.Time
	active   time.Time // the last voice or text
	tcp      voiceSeq
	reported Reported
	// Voice bytes in the current second and the one before.
	second          int64
	bytes, bytesAgo uint32
	recent          codecRing // see codecstats.go
}

func newConnStats(now time.Time) connStats {
	return connStats{since: now, active: now}
}

// voice counts a voice packet of size bytes.
func (m *connStats) voice(p audioPacket, tcp bool, size int, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tcp {
		m.tcp.add(p.frame, p.terminator)
	}
	m.active = now
	m.recent.add(p.opus)
	if sec := now.Unix(); sec != m.second {
		m.bytesAgo = 0
		if sec == m.second+1 {
			m.bytesAgo = m.bytes
		}
		m.second, m.bytes = sec, 0
	}
	m.bytes += uint32(size)
}

// bandwidth is bytes per second over the last two seconds.
func (m *connStats) bandwidth(now time.Time) uint32 {
	switch now.Unix() {
	case m.second:
		return (m.bytes + m.bytesAgo) / 2
	case m.second + 1:
		return m.bytes / 2
	}
	return 0
}

func (m *connStats) touch(now time.Time) {
	m.mu.Lock()
	m.active = now
	m.mu.Unlock()
}

// ping keeps what the client measured. Only fields that are there: the
// web bridge's keep-alive pings carry none.
func (m *connStats) ping(p *pb.Ping) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := &m.reported
	keep(&r.FromServer.Good, p.Good)
	keep(&r.FromServer.Late, p.Late)
	keep(&r.FromServer.Lost, p.Lost)
	keep(&r.FromServer.Resync, p.Resync)
	keep(&r.TCPPackets, p.TcpPackets)
	keep(&r.TCPPing.Avg, p.TcpPingAvg)
	keep(&r.TCPPing.Var, p.TcpPingVar)
	keep(&r.UDPPackets, p.UdpPackets)
	keep(&r.UDPPing.Avg, p.UdpPingAvg)
	keep(&r.UDPPing.Var, p.UdpPingVar)
}

func keep[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}

// voiceSeq counts voice frames by their frame numbers (over TCP, where no
// crypt counts them): a gap is lost frames, an older number a late frame
// (good, and no longer lost, as in the crypt); the last one again is
// ignored (over TCP nothing repeats, so no history as in the crypt).
// Mumble numbers 10 ms frames, so the step between packets is their length
// (2 for 20 ms); it is the smallest step seen in a transmission.
type voiceSeq struct {
	good, late, lost uint32
	last, step       uint64
	started          bool
}

// A jump of more steps than this (either way) starts a new transmission,
// as after a pause.
const maxFrameGap = 50

func (s *voiceSeq) add(frame uint64, terminator bool) {
	far := maxFrameGap * max(s.step, 1)
	switch {
	case s.started && frame > s.last && frame-s.last <= far:
		d := frame - s.last
		if s.step == 0 || d < s.step {
			s.step = d
		}
		s.lost += uint32(d/s.step - 1)
		s.good++
		s.last = frame
	case s.started && frame == s.last:
	case s.started && frame < s.last && s.last-frame <= far:
		s.good++
		s.late++
		if s.lost > 0 {
			s.lost--
		}
	default:
		s.good++
		s.last, s.step = frame, 0
	}
	s.started = !terminator
}

func (v VoiceStats) plus(o VoiceStats) VoiceStats {
	return VoiceStats{v.Good + o.Good, v.Late + o.Late, v.Lost + o.Lost, v.Resync + o.Resync}
}

func (v VoiceStats) msg() *pb.UserStats_Stats {
	return &pb.UserStats_Stats{Good: &v.Good, Late: &v.Late, Lost: &v.Lost, Resync: &v.Resync}
}

// userStatsMsg answers UserStats as Murmur: pings and times for all who
// may see the user; counters and bandwidth also for the same channel;
// version, address and certificates only for oneself and admins (Register
// in root), and not with stats_only. Others need Enter in the user's
// channel. Hidden users are unknown.
func (s *Server) userStatsMsg(c *client, m *pb.UserStats) {
	s.mu.Lock()
	target := s.clients[m.GetSession()]
	actor, u := s.state.Users[c.session], s.state.Users[m.GetSession()]
	if target == nil || actor == nil || u == nil || !c.sees(u) {
		s.mu.Unlock()
		return
	}
	extend := target == c || s.state.Has(actor, 0, Register)
	enter := extend || s.state.Has(actor, u.Channel, Enter)
	local := extend || u.Channel == actor.Channel
	channel, name, udp := u.Channel, u.Name, target.udpVoice
	s.mu.Unlock()
	if !enter {
		c.denyPerm(Enter, channel)
		return
	}
	st := target.connStats(name, udp, time.Now())
	out := &pb.UserStats{
		Session:    proto.Uint32(st.Session),
		TcpPackets: proto.Uint32(st.TCPPackets), TcpPingAvg: proto.Float32(st.TCPPing.Avg), TcpPingVar: proto.Float32(st.TCPPing.Var),
		UdpPackets: proto.Uint32(st.UDPPackets), UdpPingAvg: proto.Float32(st.UDPPing.Avg), UdpPingVar: proto.Float32(st.UDPPing.Var),
		Onlinesecs: proto.Uint32(uint32(st.Online / time.Second)), Idlesecs: proto.Uint32(uint32(st.Idle / time.Second)),
	}
	if extend && !m.GetStatsOnly() {
		address := st.Address.As16()
		// Certificates are not verified (see TLSConfig).
		out.Certificates, out.StrongCertificate, out.Address = target.certs, proto.Bool(false), address[:]
		if st.Version != 0 {
			out.Version = &pb.Version{
				VersionV1: proto.Uint32(legacyVersion(st.Version)), VersionV2: proto.Uint64(st.Version),
				Release: proto.String(st.Release), Os: proto.String(st.OS), OsVersion: proto.String(st.OSVersion),
			}
		}
		out.Opus = proto.Bool(target.opus)
	}
	if local {
		out.FromClient, out.FromServer = st.UDP.plus(st.TCP).msg(), st.FromServer.msg()
		out.Bandwidth = proto.Uint32(st.Bandwidth)
	}
	c.sendMsg(mumble.TypeUserStats, out)
}

// legacyVersion is version_v1 (8 bits for minor and patch) of a version_v2.
func legacyVersion(v uint64) uint32 {
	return uint32(v>>48)<<16 | uint32(min(v>>32&0xffff, 255))<<8 | uint32(min(v>>16&0xffff, 255))
}
