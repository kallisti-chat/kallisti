package mumbled

import (
	"maps"
	"slices"
)

// AudioContext tells receivers how a voice packet reached them.
type AudioContext uint32

const (
	ContextNormal  AudioContext = 0
	ContextShout   AudioContext = 1 // to a channel through a voice target
	ContextWhisper AudioContext = 2 // to a user through a voice target
	ContextListen  AudioContext = 3 // to a channel listener

	loopbackTarget = 31 // the server sends the voice back to the speaker
	maxTargetID    = 30

	maxTargetSessions = 100
	maxTargetChannels = 50
	maxListening      = 50
	// VoiceTarget messages per second; a client may set all targets at once.
	targetLimit = 10
	targetBurst = maxTargetID
	// Murmur's default bandwidth: what one user may send in voice.
	maxBandwidth = 558000 // bits per second
)

// VoiceTarget is what a whisper or shout goes to (VoiceTarget message).
type VoiceTarget struct {
	Sessions []uint32
	Channels []TargetChannel
}

type TargetChannel struct {
	ID              uint32
	Children, Links bool
	Group           string // only members of this group (as seen there)
}

type cachedReceivers struct {
	gen       uint64
	receivers map[uint32]AudioContext
}

// Changed drops all cached receivers. Call it after anything that changes
// who hears whom: moves, joins, leaves, links, ACLs, deafen, targets.
func (s *State) Changed() { s.gen++ }

// SetTarget stores a user's voice target (ids 1 to 30); an empty target
// removes it. Unknown sessions and channels and repeats are dropped.
func (s *State) SetTarget(session, id uint32, vt VoiceTarget) {
	u := s.Users[session]
	if u == nil || id < 1 || id > maxTargetID {
		return
	}
	var clean VoiceTarget
	for _, o := range vt.Sessions {
		if len(clean.Sessions) < maxTargetSessions && s.Users[o] != nil && !slices.Contains(clean.Sessions, o) {
			clean.Sessions = append(clean.Sessions, o)
		}
	}
	for _, tc := range vt.Channels {
		if len(clean.Channels) < maxTargetChannels && s.Channels[tc.ID] != nil && !slices.Contains(clean.Channels, tc) {
			clean.Channels = append(clean.Channels, tc)
		}
	}
	if u.Targets == nil {
		u.Targets = map[uint32]VoiceTarget{}
	}
	if len(clean.Sessions)+len(clean.Channels) == 0 {
		delete(u.Targets, id)
	} else {
		u.Targets[id] = clean
	}
	delete(u.cache, id) // targets only change what their owner reaches
}

// Listen starts or stops a user listening to a channel. It reports whether
// anything changed.
func (s *State) Listen(session, channel uint32, on bool) bool {
	u := s.Users[session]
	if u == nil || s.Channels[channel] == nil || u.Listening[channel] == on || (on && len(u.Listening) >= maxListening) {
		return false
	}
	if u.Listening == nil {
		u.Listening = map[uint32]bool{}
	}
	if on {
		u.Listening[channel] = true
	} else {
		delete(u.Listening, channel)
	}
	s.Changed()
	return true
}

// DropListeners stops users listening where they may no longer listen
// (after permission changes). It returns the channels each user stopped
// listening to.
func (s *State) DropListeners() map[uint32][]uint32 {
	dropped := map[uint32][]uint32{}
	for _, u := range s.Users {
		if chans := s.dropListener(u); len(chans) > 0 {
			dropped[u.Session] = chans
		}
	}
	return dropped
}

// dropListener stops one user listening where it may no longer listen
// (for example after a move, with Listen from @in). It returns those
// channels.
func (s *State) dropListener(u *User) []uint32 {
	var dropped []uint32
	for _, ch := range slices.Sorted(maps.Keys(u.Listening)) {
		if !s.Has(u, ch, Listen) && s.Listen(u.Session, ch, false) {
			dropped = append(dropped, ch)
		}
	}
	return dropped
}

// Receivers are the users who get a voice packet from sender to target
// (0: its channel, 1-30: a voice target, 31: loopback), with the context
// each gets, as Murmur's processMsg. Results are cached until Changed.
func (s *State) Receivers(sender, target uint32) map[uint32]AudioContext {
	u := s.Users[sender]
	if u == nil || u.SelfMute || u.Mute || u.Suppress || u.textOnly() {
		return nil
	}
	if target == loopbackTarget {
		return map[uint32]AudioContext{sender: ContextNormal}
	}
	if c, ok := u.cache[target]; ok && c.gen == s.gen {
		return c.receivers
	}
	out := map[uint32]AudioContext{}
	add := func(o *User, ctx AudioContext) {
		if o.Session == sender || o.Deaf || o.SelfDeaf || o.textOnly() {
			return
		}
		if _, ok := out[o.Session]; !ok {
			out[o.Session] = ctx
		}
	}
	if target == 0 {
		speak := s.speakChannels(u)
		// Over links only to those who see the speaker's channel.
		sees := func(o *User, ch uint32) bool { return ch == u.Channel || s.Sees(o, u.Channel) }
		for _, o := range s.Users {
			if speak[o.Channel] && sees(o, o.Channel) {
				add(o, ContextNormal)
			}
		}
		for _, o := range s.Users {
			for ch := range o.Listening {
				if speak[ch] && sees(o, ch) {
					add(o, ContextListen)
				}
			}
		}
	} else if vt, ok := u.Targets[target]; ok {
		s.addTargetReceivers(u, vt, add)
	}
	if u.cache == nil {
		u.cache = map[uint32]cachedReceivers{}
	}
	u.cache[target] = cachedReceivers{gen: s.gen, receivers: out}
	return out
}

// addTargetReceivers adds whisper and shout receivers: users in the target
// channels where the speaker may whisper and speak (filtered by group), then users
// named directly, then listeners of the target channels.
func (s *State) addTargetReceivers(u *User, vt VoiceTarget, add func(*User, AudioContext)) {
	// Target channel: its group filters, any of which lets a user in ("" is
	// everyone). Whisper is checked once per channel.
	groups := map[uint32][]string{}
	may := map[uint32]bool{}
	for _, tc := range vt.Channels {
		// A channel removed since the target was set (a temporary one).
		if s.Channels[tc.ID] == nil {
			continue
		}
		chans := map[uint32]bool{tc.ID: true}
		if tc.Links {
			chans = s.linked(tc.ID)
		}
		if tc.Children {
			for _, sub := range s.Subtree(tc.ID) {
				chans[sub] = true
			}
		}
		for ch := range chans {
			ok, seen := may[ch]
			if !seen {
				// Speak too (not in Murmur): a shout must not get around
				// a listen-only channel or a time-out there.
				ok = s.Has(u, ch, Whisper|Speak)
				may[ch] = ok
			}
			if ok && !slices.Contains(groups[ch], tc.Group) {
				groups[ch] = append(groups[ch], tc.Group)
			}
		}
	}
	member := func(o *User, ch uint32) bool {
		return slices.ContainsFunc(groups[ch], func(g string) bool {
			return g == "" || s.inGroup(s.Channels[ch], s.Channels[ch], g, o)
		})
	}
	for _, o := range s.Users {
		if member(o, o.Channel) {
			add(o, ContextShout)
		}
	}
	for _, id := range vt.Sessions {
		if o := s.Users[id]; o != nil && s.Has(u, o.Channel, Whisper) {
			add(o, ContextWhisper)
		}
	}
	for _, o := range s.Users {
		for ch := range o.Listening {
			if member(o, ch) {
				add(o, ContextListen)
			}
		}
	}
}
