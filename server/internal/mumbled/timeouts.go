package mumbled

import (
	"cmp"
	"errors"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble/pb"
)

// Time-outs (Kallisti, not Mumble): a timed out user may not send texts
// and stays server-muted until the time ends. Registered users by user id
// (saved: they last over reconnects and restarts), guests by session
// (until they leave). Moderators and admins (MuteDeafen in root: a
// time-out is a mute everywhere) give them, as kicks: never to SuperUser,
// to staff only by admins.
//
// Channel time-outs take Speak, Whisper and TextMessage in one channel and
// its subchannels (see State.Permissions), and no mute; the channel's
// moderators give them too (see mayActOnIn).

const maxTimeout = 28 * 24 * time.Hour

// timeoutKey: with inChannel, a channel time-out.
type timeoutKey struct {
	guest     bool
	id        uint32 // the user id, or a guest's session
	inChannel bool
	channel   uint32
}

func timeoutKeyOf(u *User) timeoutKey {
	if u.Registered {
		return timeoutKey{id: u.UserID}
	}
	return timeoutKey{guest: true, id: u.Session}
}

func channelTimeoutKey(u *User, channel uint32) timeoutKey {
	k := timeoutKeyOf(u)
	k.inChannel, k.channel = true, channel
	return k
}

// keepMute: the user was server-muted before; the end does not unmute.
type timeout struct {
	until    time.Time
	timer    *time.Timer
	keepMute bool
}

// Timeout is a running time-out: of a registered user (UserID) or a guest
// (Session); Channel for a channel time-out.
type Timeout struct {
	Registered bool
	UserID     uint32
	Session    uint32
	Channel    uint32
	Until      time.Time
}

// loadTimeouts starts the saved time-outs that did not end yet (channel
// time-outs of channels that are still there).
func (s *Server) loadTimeouts() error {
	s.timeouts = map[timeoutKey]*timeout{}
	if s.cfg.DB == nil {
		return nil
	}
	saved, err := s.cfg.DB.Timeouts()
	if err != nil {
		return err
	}
	inChannels, err := s.cfg.DB.ChannelTimeouts()
	if err != nil {
		return err
	}
	keys := map[timeoutKey]time.Time{}
	for id, until := range saved {
		keys[timeoutKey{id: id}] = until
	}
	for uc, until := range inChannels {
		keys[timeoutKey{id: uc.User, inChannel: true, channel: uc.Channel}] = until
	}
	for key, until := range keys {
		if until.After(time.Now()) && (!key.inChannel || s.state.Channels[key.channel] != nil) {
			s.startTimeoutLocked(key, until)
		} else {
			s.saveTimeout(key, time.Time{})
		}
	}
	return nil
}

// SetTimeout times a user out for d, or ends its time-out (d 0).
func (s *Server) SetTimeout(actor, session uint32, d time.Duration) error {
	return s.setTimeout(actor, session, d, func(a, u *User) (timeoutKey, bool) {
		return timeoutKeyOf(u), s.mayActOn(a, u) && s.state.Has(a, 0, MuteDeafen)
	})
}

// SetChannelTimeout times a user out in a channel (not root) for d, or ends
// it (d 0). Staff and the channel's moderators may.
func (s *Server) SetChannelTimeout(actor, session, channel uint32, d time.Duration) error {
	return s.setTimeout(actor, session, d, func(a, u *User) (timeoutKey, bool) {
		ok := channel != 0 && channel != s.textParent && s.state.Channels[channel] != nil
		return channelTimeoutKey(u, channel), ok && s.state.moderates(a, channel) && s.mayActOnIn(a, u, channel)
	})
}

// setTimeout sets the time-out that key names, if may (under s.mu).
func (s *Server) setTimeout(actor, session uint32, d time.Duration, may func(a, u *User) (timeoutKey, bool)) error {
	if d < 0 || d > maxTimeout {
		return errors.New("invalid time-out")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	a, u := s.state.Users[actor], s.state.Users[session]
	var key timeoutKey
	allowed := false
	if a != nil && u != nil {
		key, allowed = may(a, u)
	}
	switch {
	case a == nil || u == nil:
		s.mu.Unlock()
		return errors.New("no such user")
	case a == u:
		s.mu.Unlock()
		return errors.New("you cannot time out yourself")
	case !allowed:
		s.mu.Unlock()
		return errors.New("you may not time out this user")
	case s.timeouts[key] == nil && d == 0:
		s.mu.Unlock()
		return errors.New("this user is not timed out")
	}
	var until time.Time
	if d > 0 {
		until = time.Now().Add(d)
	}
	s.setTimeoutLocked(key, until, actor)
	s.mu.Unlock()
	s.saveTimeout(key, until)
	s.timeoutsChanged()
	return nil
}

// expireTimeout ends a time-out when its time is up (if it was not
// changed meanwhile).
func (s *Server) expireTimeout(key timeoutKey, until time.Time) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	t := s.timeouts[key]
	if t == nil || !t.until.Equal(until) || s.closing.Load() {
		s.mu.Unlock()
		return
	}
	s.setTimeoutLocked(key, time.Time{}, 0)
	s.mu.Unlock()
	s.saveTimeout(key, time.Time{})
	s.timeoutsChanged()
}

// setTimeoutLocked replaces a time-out (zero: none) and mutes or unmutes
// the user if online; a channel time-out changes permissions. actor 0:
// the server.
func (s *Server) setTimeoutLocked(key timeoutKey, until time.Time, actor uint32) {
	old := s.timeouts[key]
	keep := old != nil && old.keepMute
	s.dropTimeoutLocked(key)
	on := !until.IsZero()
	if key.inChannel {
		if on {
			s.startTimeoutLocked(key, until)
		}
		s.permissionsChangedLocked(nil) // suppress states follow Speak
		return
	}
	var online []*User // every session of an account (see textonly.go)
	for _, u := range s.state.Users {
		if timeoutKeyOf(u) == key {
			online = append(online, u)
		}
	}
	if on {
		if old == nil && len(online) > 0 {
			keep = slices.ContainsFunc(online, func(u *User) bool { return u.Mute && !u.textOnly() })
		}
		s.startTimeoutLocked(key, until)
		s.timeouts[key].keepMute = keep
	}
	if !on && keep {
		return
	}
	for _, u := range online {
		if u.Mute == on {
			continue
		}
		u.Mute, u.Deaf = serverMuteState(u.Mute, u.Deaf, &pb.UserState{Mute: proto.Bool(on)})
		out := &pb.UserState{Session: proto.Uint32(u.Session), Mute: proto.Bool(u.Mute), Deaf: proto.Bool(u.Deaf)}
		if actor != 0 {
			out.Actor = proto.Uint32(actor)
		}
		s.state.Changed()
		s.sendUserLocked(u, out)
	}
}

func (s *Server) startTimeoutLocked(key timeoutKey, until time.Time) {
	s.timeouts[key] = &timeout{until: until, timer: time.AfterFunc(time.Until(until), func() { s.expireTimeout(key, until) })}
}

// dropTimeoutLocked forgets a time-out; it reports whether there was one.
func (s *Server) dropTimeoutLocked(key timeoutKey) bool {
	t := s.timeouts[key]
	if t != nil {
		t.timer.Stop()
		delete(s.timeouts, key)
	}
	return t != nil
}

// timeoutKeysLocked: a user's time-outs (server and channels), by its key.
func (s *Server) timeoutKeysLocked(user timeoutKey) []timeoutKey {
	var out []timeoutKey
	for k := range s.timeouts {
		if k.guest == user.guest && k.id == user.id {
			out = append(out, k)
		}
	}
	return out
}

// timedOutLocked: the user's time-out, if any.
func (s *Server) timedOutLocked(u *User) (time.Time, bool) {
	if t := s.timeouts[timeoutKeyOf(u)]; t != nil {
		return t.until, true
	}
	return time.Time{}, false
}

// timedOutInLocked: the user's channel time-out in a channel or one above
// it, if any (the one that ends last).
func (s *Server) timedOutInLocked(u *User, channel uint32) (time.Time, bool) {
	var until time.Time
	if len(s.timeouts) == 0 { // the common case: Permissions asks often
		return until, false
	}
	for _, ch := range s.state.chain(channel) {
		if t := s.timeouts[channelTimeoutKey(u, ch.ID)]; t != nil && t.until.After(until) {
			until = t.until
		}
	}
	return until, !until.IsZero()
}

// TimedOut: the session's user is timed out, until when.
func (s *Server) TimedOut(session uint32) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.userLocked(session); u != nil {
		return s.timedOutLocked(u)
	}
	return time.Time{}, false
}

// ChannelTimedOut: the session's user is timed out in the channel (or one
// above it), until when.
func (s *Server) ChannelTimedOut(session, channel uint32) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.userLocked(session); u != nil {
		return s.timedOutInLocked(u, channel)
	}
	return time.Time{}, false
}

// Timeouts lists the running time-outs (registered users first);
// ChannelTimeouts the channel time-outs of channels that are there.
func (s *Server) Timeouts() []Timeout { return s.timeoutList(false) }

func (s *Server) ChannelTimeouts() []Timeout { return s.timeoutList(true) }

func (s *Server) timeoutList(inChannel bool) []Timeout {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Timeout{}
	for k, t := range s.timeouts {
		if k.inChannel != inChannel || inChannel && s.state.Channels[k.channel] == nil {
			continue
		}
		if k.guest {
			out = append(out, Timeout{Session: k.id, Channel: k.channel, Until: t.until})
		} else {
			out = append(out, Timeout{Registered: true, UserID: k.id, Channel: k.channel, Until: t.until})
		}
	}
	// Registered users have no session here: they come first.
	slices.SortFunc(out, func(a, b Timeout) int {
		return cmp.Or(cmp.Compare(a.Session, b.Session), cmp.Compare(a.UserID, b.UserID), cmp.Compare(a.Channel, b.Channel))
	})
	return out
}

// timedOutText is the refusal for a timed out user (in a channel, if
// named).
func timedOutText(who string, until time.Time, channel ...string) error {
	where := ""
	if len(channel) > 0 {
		where = " in " + channel[0]
	}
	return deny(pb.PermissionDenied_Text, "%s timed out%s until %s UTC", who, where, until.UTC().Format("2006-01-02 15:04"))
}

// saveTimeout keeps a registered user's time-out (zero: none). Callers
// hold saveMu.
func (s *Server) saveTimeout(key timeoutKey, until time.Time) {
	if key.guest || s.cfg.DB == nil {
		return
	}
	var err error
	if key.inChannel {
		err = s.cfg.DB.SaveChannelTimeout(UserChannel{User: key.id, Channel: key.channel}, until)
	} else {
		err = s.cfg.DB.SaveTimeout(key.id, until)
	}
	if err != nil {
		slog.Error("saving a time-out", "user", key.id, "err", err)
	}
}

// timeoutsChanged tells the Kallisti server (not under s.mu).
func (s *Server) timeoutsChanged() {
	if s.cfg.OnTimeoutsChanged != nil {
		s.cfg.OnTimeoutsChanged()
	}
}

// stopTimeouts stops the timers (the server closes).
func (s *Server) stopTimeouts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.timeouts {
		t.timer.Stop()
	}
}
