package mumbled

import (
	"errors"
	"net/netip"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// BanEntry refuses connections by address range, by certificate, or both.
type BanEntry struct {
	Prefix   netip.Prefix // invalid: no address ban
	Hash     string
	Name     string // the user's name when banned, for the list
	Reason   string
	Start    time.Time
	Duration time.Duration // 0: forever
}

func (b BanEntry) expired(now time.Time) bool {
	return b.Duration > 0 && now.After(b.Start.Add(b.Duration))
}

// Matches reports whether the ban applies to a connection now.
func (b BanEntry) Matches(ip netip.Addr, hash string, now time.Time) bool {
	if b.expired(now) {
		return false
	}
	return (b.Prefix.IsValid() && b.Prefix.Contains(ip)) || (b.Hash != "" && b.Hash == hash)
}

// banPrefix reads an address as Mumble sends it: 16 bytes (IPv4 mapped
// into IPv6) and a mask out of 128 bits.
func banPrefix(addr []byte, mask uint32) (netip.Prefix, bool) {
	if len(addr) != 16 || mask > 128 {
		return netip.Prefix{}, false
	}
	a := netip.AddrFrom16([16]byte(addr))
	bits := int(mask)
	if a.Is4In6() {
		if bits < 96 {
			return netip.Prefix{}, false
		}
		a, bits = a.Unmap(), bits-96
	}
	return netip.PrefixFrom(a, bits).Masked(), true
}

// banAddress is the other way round.
func banAddress(p netip.Prefix) ([]byte, uint32) {
	a := p.Addr().As16()
	bits := uint32(p.Bits())
	if p.Addr().Is4() {
		bits += 96
	}
	return a[:], bits
}

// userRemoveMsg kicks (Kick or Ban in root) or bans (Ban) a user. A ban
// covers its certificate and address, as Murmur before 1.6.
func (s *Server) userRemoveMsg(c *client, m *pb.UserRemove) {
	if missing, _ := s.removeUser(c.session, m, 0); missing != 0 {
		c.denyPerm(missing, 0)
	}
}

// Ban bans a user for a time (0: forever), by certificate and address:
// Mumble's UserRemove has no duration (for the web client).
func (s *Server) Ban(actor, session uint32, reason string, d time.Duration) error {
	if d < 0 {
		return errors.New("invalid ban duration")
	}
	missing, done := s.removeUser(actor, &pb.UserRemove{Session: proto.Uint32(session), Reason: proto.String(reason), Ban: proto.Bool(true)}, d)
	switch {
	case missing != 0:
		return errors.New("you may not ban this user")
	case !done:
		return errors.New("cannot ban this user (gone, or nothing to ban by)")
	}
	return nil
}

// removeUser kicks or bans (for d, 0: forever); it returns the missing
// permission, or whether it was done.
func (s *Server) removeUser(actorSession uint32, m *pb.UserRemove, d time.Duration) (missing Perm, done bool) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	actor, u := s.state.Users[actorSession], s.state.Users[m.GetSession()]
	if actor == nil || u == nil {
		s.mu.Unlock()
		return 0, false
	}
	need := Kick | Ban // either one
	if m.GetBan() {
		need = Ban
	}
	if !s.mayActOn(actor, u) || s.state.Permissions(actor, 0)&need == 0 {
		s.mu.Unlock()
		return need, false
	}
	var bans []BanEntry // also the certificates and addresses of the account's other sessions
	if m.GetBan() {
		// Clients before 1.6 send neither option: both, as Murmur.
		byCert := m.BanCertificate == nil || m.GetBanCertificate()
		byIP := m.BanIp == nil || m.GetBanIp()
		// entries ban a session by what is not banned yet: its address, its
		// identity's certificate and a Mumble client's own (another one
		// after a password login).
		start := time.Now().UTC().Truncate(time.Second)
		hashes, hosts := map[string]bool{"": true}, map[netip.Prefix]bool{{}: true}
		entries := func(o *User) (out []BanEntry) {
			b := BanEntry{Name: u.Name, Reason: m.GetReason(), Start: start, Duration: d}
			t := s.clients[o.Session]
			if byIP && t != nil && t.host.IsValid() {
				if p := netip.PrefixFrom(t.host, t.host.BitLen()); !hosts[p] {
					b.Prefix, hosts[p] = p, true
				}
			}
			var own string
			if t != nil {
				own = certHash(t.certs)
			}
			for _, h := range []string{o.Hash, own} {
				if byCert && !hashes[h] {
					hashes[h] = true
					e := b
					e.Hash, b.Prefix = h, netip.Prefix{} // the address once
					out = append(out, e)
				}
			}
			if b.Prefix.IsValid() {
				out = append(out, b)
			}
			return out
		}
		if bans = entries(u); len(bans) == 0 {
			s.mu.Unlock()
			return 0, false // nothing to ban by
		}
		for _, o := range s.siblingsLocked(u) {
			bans = append(bans, entries(o)...)
		}
	}
	// Every session of the account goes (see textonly.go).
	for _, o := range append(s.siblingsLocked(u), u) {
		s.kickLocked(o.Session, actorSession, m.GetReason(), bans != nil)
	}
	if bans != nil {
		// Under the lock: a connection that passed the ban checks meets
		// the ban in join.
		bans = s.publishBans(append(slices.Clone(s.currentBans()), bans...))
	}
	s.mu.Unlock()
	if bans != nil {
		s.saveBans(actorSession, bans)
	}
	return 0, true
}

// mayActOn: nobody acts on SuperUser, only admins (Write in root) on staff.
func (s *Server) mayActOn(actor, u *User) bool {
	return !isSuperUser(u) && (!s.state.isStaff(u) || s.state.Has(actor, 0, Write))
}

// currentBans is the ban list; callers must not change it.
func (s *Server) currentBans() []BanEntry {
	if p := s.bans.Load(); p != nil {
		return *p
	}
	return nil
}

// setBans replaces the ban list without expired bans, and saves it.
// Callers hold saveMu.
func (s *Server) setBans(actor uint32, bans []BanEntry) {
	s.saveBans(actor, s.publishBans(bans))
}

// publishBans replaces the ban list without expired bans and returns it.
// Callers hold saveMu, and save the list (saveBans) in the same order.
func (s *Server) publishBans(bans []BanEntry) []BanEntry {
	now := time.Now()
	bans = slices.DeleteFunc(slices.Clone(bans), func(b BanEntry) bool { return b.expired(now) })
	s.bans.Store(&bans)
	return bans
}

func (s *Server) saveBans(actor uint32, bans []BanEntry) {
	if s.cfg.DB == nil {
		return
	}
	if err := s.cfg.DB.SaveBans(bans); err != nil {
		s.notSaved(actor, "saving bans", err)
	}
}

// maxBans limits the list a client can set.
const maxBans = 5000

// banListMsg shows the ban list (Ban in root) or replaces it (Write in
// root too: moderators must not ban admins or everyone at once).
func (s *Server) banListMsg(c *client, m *pb.BanList) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	need := Ban
	if !m.GetQuery() {
		need |= Write
	}
	s.mu.Lock()
	actor := s.state.Users[c.session]
	allowed := actor != nil && s.state.Has(actor, 0, need)
	s.mu.Unlock()
	if !allowed {
		c.denyPerm(need, 0)
		return
	}
	if m.GetQuery() {
		out := &pb.BanList{}
		for _, b := range s.currentBans() {
			e := &pb.BanList_BanEntry{
				Name: proto.String(b.Name), Hash: proto.String(b.Hash), Reason: proto.String(b.Reason),
				Start: proto.String(b.Start.UTC().Format("2006-01-02T15:04:05")), Duration: proto.Uint32(uint32(b.Duration / time.Second)),
				Address: make([]byte, 16), Mask: proto.Uint32(0),
			}
			if b.Prefix.IsValid() {
				addr, mask := banAddress(b.Prefix)
				e.Address, e.Mask = addr, proto.Uint32(mask)
			}
			out.Bans = append(out.Bans, e)
		}
		c.sendMsg(mumble.TypeBanList, out)
		return
	}
	bans := []BanEntry{}
	seen := map[BanEntry]bool{}
	for _, e := range m.Bans[:min(len(m.Bans), maxBans)] {
		b := BanEntry{Hash: e.GetHash(), Name: e.GetName(), Reason: e.GetReason(),
			Duration: time.Duration(e.GetDuration()) * time.Second, Start: time.Now().UTC().Truncate(time.Second)}
		if p, ok := banPrefix(e.Address, e.GetMask()); ok && e.GetMask() > 0 {
			b.Prefix = p
		}
		if e.Start != nil {
			if t, ok := parseBanStart(e.GetStart()); ok {
				b.Start = t
			}
		}
		if (!b.Prefix.IsValid() && b.Hash == "") || seen[b] {
			continue
		}
		seen[b] = true
		bans = append(bans, b)
	}
	s.setBans(c.session, bans)
}

// parseBanStart reads Qt's ISO dates (UTC, often without a zone).
func parseBanStart(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
