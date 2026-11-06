package mumbled

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"kallisti.chat/server/internal/mumble/pb"
	"kallisti.chat/server/internal/names"
)

// State is the server's channels and users, without IO. The server holds
// its lock around every call.
type State struct {
	Channels    map[uint32]*Channel
	Users       map[uint32]*User
	nextChannel uint32
	gen         uint64 // see Changed
	// held: a temporary channel that stays while empty (see
	// takeVoiceLocked, which does not nest); 0 (root, never temporary)
	// for none.
	held uint32
	// Guests may not write (see guests.go).
	GuestsReadOnly bool
	// Server-wide voice and stream quality limits (see quality.go).
	QualityLimits QualityLimits
	// Index in CaptchaLevels (see captcha.go).
	Captcha uint32
	// OnMove hears every change of a user's channel; NoChannel stands for
	// joining or leaving the server, TextOnly for no channel (see
	// textonly.go). It runs under the server lock.
	OnMove func(session, from, to uint32)
	// TimedOutIn: the user has a channel time-out in the channel or one
	// above it (see timeouts.go). It runs under the server lock.
	TimedOutIn func(u *User, channel uint32) bool
	// Mirror: the channel is a text channel mirror, their parent or below
	// them (see mirrors.go). It runs under the server lock.
	Mirror func(channel uint32) bool
}

// NoChannel: not on the server (see State.OnMove).
const NoChannel = ^uint32(0)

func (s *State) moved(session, from, to uint32) {
	if s.OnMove != nil && from != to {
		s.OnMove(session, from, to)
	}
}

type Channel struct {
	ID, Parent  uint32
	Name        string
	Description string
	Position    int32
	Temporary   bool
	Links       map[uint32]bool

	NoInheritACL bool // the zero value inherits, as new Mumble channels do
	ACLs         []ACL
	Groups       map[string]*Group
}

type User struct {
	Session uint32
	Name    string
	Channel uint32
	// The zero value is a guest; UserID counts only if Registered
	// (SuperUser is registered with id 0).
	Registered bool
	UserID     uint32
	// A Kallisti guest account: registered, but a guest (see
	// State.GuestsReadOnly).
	GuestAccount bool
	// The registration (zero: before it was kept, see msgrules.Author).
	Since  time.Time
	Hash   string   // client certificate, or ""
	Tokens []string // access tokens (Authenticate.tokens)

	Targets   map[uint32]VoiceTarget // whisper and shout targets by id
	Listening map[uint32]bool        // channels it listens to

	cache map[uint32]cachedReceivers // receivers by voice target

	SelfMute, SelfDeaf bool
	Recording          bool
	PluginContext      []byte // the game: positions go only to the same one
	Comment            string
	Texture            []byte // avatar
	// Set by moderators or the server.
	Mute, Deaf, Suppress bool
	PrioritySpeaker      bool
}

// DenyError is an action the server refuses, sent as PermissionDenied.
type DenyError struct {
	Type   pb.PermissionDenied_DenyType
	Reason string
}

func (e *DenyError) Error() string { return e.Reason }

func deny(t pb.PermissionDenied_DenyType, format string, args ...any) error {
	return &DenyError{Type: t, Reason: fmt.Sprintf(format, args...)}
}

// Murmur's default channel name rule (Qt's \w includes all letters), with
// a length limit.
var channelName = regexp.MustCompile(`^[ \-=\p{L}\p{N}\p{M}_#\[\]{}()@|]+$`)

const maxChannelName = 100

// NewState makes the state with the root channel and saved channels
// (parents may come after children). Root keeps the configured name;
// channels whose parent is missing move below root.
func NewState(rootName string, saved []Channel) *State {
	s := &State{Channels: map[uint32]*Channel{}, Users: map[uint32]*User{}}
	s.Channels[0] = &Channel{ID: 0, Name: rootName, Links: map[uint32]bool{}}
	for _, c := range saved {
		if c.ID == 0 {
			root := s.Channels[0]
			root.Description, root.Position = c.Description, c.Position
			root.NoInheritACL, root.ACLs, root.Groups = c.NoInheritACL, c.ACLs, c.Groups
			continue
		}
		c := c
		c.Links = map[uint32]bool{}
		s.Channels[c.ID] = &c
		s.nextChannel = max(s.nextChannel, c.ID)
	}
	// Missing parents first, then loops, in a fixed order.
	for _, c := range s.Channels {
		if s.Channels[c.Parent] == nil {
			c.Parent = 0
		}
	}
	for _, id := range slices.Sorted(maps.Keys(s.Channels)) {
		if !s.reachesRoot(id) {
			s.Channels[id].Parent = 0
		}
	}
	for _, c := range saved {
		for l := range c.Links {
			s.Link(c.ID, l)
		}
	}
	return s
}

// reachesRoot is false for channels whose parent chain is broken.
func (s *State) reachesRoot(id uint32) bool {
	for range len(s.Channels) {
		c := s.Channels[id]
		if c == nil {
			return false
		}
		if id == 0 {
			return true
		}
		id = c.Parent
	}
	return false // a loop
}

// SetLastChannelID makes new channels get higher ids (ids are not reused).
func (s *State) SetLastChannelID(id uint32) { s.nextChannel = max(s.nextChannel, id) }

// LastChannelID is the highest id given out.
func (s *State) LastChannelID() uint32 { return s.nextChannel }

func (s *State) checkName(parent uint32, name string, self uint32) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > maxChannelName || !channelName.MatchString(name) {
		return deny(pb.PermissionDenied_ChannelName, "invalid channel name")
	}
	for _, c := range s.Channels {
		if c.Parent == parent && c.ID != self && c.ID != 0 && names.Key(c.Name) == names.Key(name) {
			return deny(pb.PermissionDenied_ChannelName, "a channel with this name exists here")
		}
	}
	return nil
}

func (s *State) AddChannel(parent uint32, name string, temporary bool) (*Channel, error) {
	p := s.Channels[parent]
	if p == nil {
		return nil, deny(pb.PermissionDenied_Text, "no such channel")
	}
	if p.Temporary {
		return nil, deny(pb.PermissionDenied_TemporaryChannel, "temporary channels have no subchannels")
	}
	name = strings.TrimSpace(name)
	if err := s.checkName(parent, name, 0); err != nil {
		return nil, err
	}
	s.nextChannel++
	c := &Channel{ID: s.nextChannel, Parent: parent, Name: name, Temporary: temporary, Links: map[uint32]bool{}}
	s.Channels[c.ID] = c
	return c, nil
}

// ChannelEdit holds the fields to change; nil means unchanged.
type ChannelEdit struct {
	Name        *string
	Description *string
	Position    *int32
	Parent      *uint32
}

func (s *State) isBelow(id, ancestor uint32) bool {
	for id != 0 {
		if id == ancestor {
			return true
		}
		id = s.Channels[id].Parent
	}
	return ancestor == 0
}

func (s *State) EditChannel(id uint32, e ChannelEdit) error {
	c := s.Channels[id]
	if c == nil {
		return deny(pb.PermissionDenied_Text, "no such channel")
	}
	parent := c.Parent
	if e.Parent != nil {
		p := s.Channels[*e.Parent]
		if id == 0 || p == nil || s.isBelow(*e.Parent, id) {
			return deny(pb.PermissionDenied_Text, "cannot move the channel there")
		}
		if p.Temporary {
			return deny(pb.PermissionDenied_TemporaryChannel, "temporary channels have no subchannels")
		}
		parent = *e.Parent
	}
	name := c.Name
	if e.Name != nil {
		if id == 0 {
			return deny(pb.PermissionDenied_ChannelName, "the root channel has the server's name")
		}
		name = strings.TrimSpace(*e.Name)
	}
	if e.Description != nil && len(*e.Description) > maxTextLength {
		return deny(pb.PermissionDenied_TextTooLong, "the description is too long")
	}
	if id != 0 && (e.Name != nil || e.Parent != nil) {
		if err := s.checkName(parent, name, id); err != nil {
			return err
		}
	}
	if parent != c.Parent {
		s.Changed() // inherited ACLs change
	}
	c.Parent, c.Name = parent, name
	if e.Description != nil {
		c.Description = *e.Description
	}
	if e.Position != nil {
		c.Position = *e.Position
	}
	return nil
}

// Subtree is the channel and everything below it, children first.
func (s *State) Subtree(id uint32) []uint32 {
	var out []uint32
	for _, c := range s.Channels {
		if c.Parent == id && c.ID != 0 {
			out = append(out, s.Subtree(c.ID)...)
		}
	}
	slices.Sort(out) // stable order for tests and clients
	return append(out, id)
}

// RemoveChannel removes a channel and its subchannels. Their users move to
// the channel's parent. It returns the moves and the removed channels,
// children first.
func (s *State) RemoveChannel(id uint32) (moved map[uint32]uint32, removed []uint32, err error) {
	c := s.Channels[id]
	if c == nil || id == 0 {
		return nil, nil, deny(pb.PermissionDenied_Text, "cannot remove this channel")
	}
	removed = s.Subtree(id)
	gone := map[uint32]bool{}
	for _, r := range removed {
		gone[r] = true
	}
	moved = map[uint32]uint32{}
	for _, u := range s.Users {
		if gone[u.Channel] {
			s.moved(u.Session, u.Channel, c.Parent)
			u.Channel = c.Parent
			moved[u.Session] = c.Parent
		}
	}
	for _, r := range removed {
		for l := range s.Channels[r].Links {
			s.Unlink(r, l)
		}
		delete(s.Channels, r)
		for _, u := range s.Users {
			delete(u.Listening, r)
		}
	}
	s.Changed()
	return moved, removed, nil
}

// Refresh recomputes what a user may say where: suppressed without Speak
// in its channel (as Murmur; text-only sessions have no voice anyway),
// and the channels its voice reaches.
func (s *State) Refresh(u *User) {
	u.Suppress = !u.textOnly() && !s.Has(u, u.Channel, Speak)
	s.Changed()
}

// speakChannels: as Murmur, voice goes to linked channels only where the
// speaker may speak.
func (s *State) speakChannels(u *User) map[uint32]bool {
	out := map[uint32]bool{}
	for ch := range s.linked(u.Channel) {
		if ch == u.Channel || s.Has(u, ch, Speak) {
			out[ch] = true
		}
	}
	return out
}

func (s *State) Link(a, b uint32) {
	if a == b || s.Channels[a] == nil || s.Channels[b] == nil {
		return
	}
	s.Changed()
	s.Channels[a].Links[b] = true
	s.Channels[b].Links[a] = true
}

func (s *State) Unlink(a, b uint32) {
	s.Changed()
	if s.Channels[a] != nil {
		delete(s.Channels[a].Links, b)
	}
	if s.Channels[b] != nil {
		delete(s.Channels[b].Links, a)
	}
}

// Tree lists the channels with parents before children.
func (s *State) Tree() []*Channel {
	var out []*Channel
	var walk func(id uint32)
	walk = func(id uint32) {
		out = append(out, s.Channels[id])
		var children []*Channel
		for _, c := range s.Channels {
			if c.Parent == id && c.ID != 0 {
				children = append(children, c)
			}
		}
		slices.SortFunc(children, func(a, b *Channel) int { return int(a.ID) - int(b.ID) })
		for _, c := range children {
			walk(c.ID)
		}
	}
	walk(0)
	return out
}

func (s *State) AddUser(u *User) {
	s.Users[u.Session] = u
	s.Changed()
	s.moved(u.Session, NoChannel, u.Channel)
}

// RemoveUser returns temporary channels that became empty and were removed.
func (s *State) RemoveUser(session uint32) []uint32 {
	u := s.Users[session]
	if u == nil {
		return nil
	}
	delete(s.Users, session)
	s.Changed()
	s.moved(session, u.Channel, NoChannel)
	return s.removeIfEmptyTemporary(u.Channel)
}

// MoveUser returns temporary channels that became empty and were removed.
func (s *State) MoveUser(session, channel uint32) []uint32 {
	u := s.Users[session]
	if u == nil || s.Channels[channel] == nil && channel != TextOnly || u.Channel == channel {
		return nil
	}
	old := u.Channel
	u.Channel = channel
	s.Changed()
	s.moved(session, old, channel)
	return s.removeIfEmptyTemporary(old)
}

func (s *State) removeIfEmptyTemporary(id uint32) []uint32 {
	c := s.Channels[id]
	if c == nil || !c.Temporary || id == s.held {
		return nil
	}
	for _, u := range s.Users {
		if u.Channel == id {
			return nil
		}
	}
	for _, o := range s.Channels {
		if o.Parent == id && o.ID != 0 {
			return nil
		}
	}
	_, removed, _ := s.RemoveChannel(id)
	return removed
}

// linked is the channel and every channel reachable over links.
func (s *State) linked(id uint32) map[uint32]bool {
	seen := map[uint32]bool{id: true}
	queue := []uint32{id}
	for len(queue) > 0 {
		c := s.Channels[queue[0]]
		queue = queue[1:]
		if c == nil {
			continue // removed (links are cleaned up, but stay safe)
		}
		for l := range c.Links {
			if !seen[l] {
				seen[l] = true
				queue = append(queue, l)
			}
		}
	}
	return seen
}

// Listeners are the users who hear the sender talk to its channel.
func (s *State) Listeners(sender uint32) []uint32 {
	return slices.Sorted(maps.Keys(s.Receivers(sender, 0)))
}
