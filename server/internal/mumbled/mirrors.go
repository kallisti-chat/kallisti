package mumbled

import (
	"html"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

// Text channel mirrors: the Kallisti server's text channels as Mumble
// channels under one parent, so Mumble clients can read and write them.
// Nobody speaks, edits or builds there; the Kallisti server owns them.

const (
	textParentName = "Text channels"
	// After the voice channels in clients' trees.
	textParentPosition = 1000
	mirrorDeny         = Speak | Write | MakeChannel | MakeTempChannel | LinkChannel // see mirrorACL
)

// TextMirror is one wanted mirror: its Mumble channel (0: make one) and
// the text channel's name.
type TextMirror struct {
	Channel uint32
	Name    string
	Topic   string // plain text; the mirror's description
	// A new mirror is private (only staff see it); an existing one keeps
	// its access (see SetPrivate).
	Private bool
	// Its category: an index into the categories, plus one (0: none).
	Category int
}

// TextCategory is one wanted category: its Mumble channel (0: make one),
// which holds its mirrors (so its moderators' group and time-outs hold
// there too), and its name.
type TextCategory struct {
	Channel uint32
	Name    string
}

// SyncTextChannels is SyncTextTree without categories.
func (s *Server) SyncTextChannels(want []TextMirror) ([]uint32, error) {
	_, ids, err := s.SyncTextTree(nil, want)
	return ids, err
}

// SyncTextTree makes the mirrors match: under the parent the mirrors
// without a category, then the categories with theirs, each in order. It
// returns the Mumble channels of both. Missing ones (or a missing parent)
// are made anew; a mirror moves when its category changes.
func (s *Server) SyncTextTree(cats []TextCategory, want []TextMirror) (catIDs, ids []uint32, err error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	saved, err := s.loadTextParent()
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	var save []*Channel
	var removed []uint32
	touched := map[uint32]bool{}
	inherited := false // moves and category entries: what channels inherit changed
	parent := s.state.Channels[saved]
	if saved == 0 || parent == nil || parent.Parent != 0 {
		// One from before whose id was not saved, or a new one.
		parent = nil
		for _, ch := range s.state.Channels {
			if ch.Parent == 0 && ch.ID != 0 && ch.Name == textParentName {
				parent = ch
			}
		}
		if parent == nil {
			if parent, err = s.state.AddChannel(0, textParentName, false); err != nil {
				s.mu.Unlock()
				return nil, nil, err
			}
			parent.Position = textParentPosition
		}
		touched[parent.ID] = true
	}
	if !slices.Equal(parent.ACLs, mirrorACL()) {
		parent.ACLs = mirrorACL()
		touched[parent.ID] = true
	}
	s.textParent = parent.ID
	keepCat, keep := map[uint32]bool{}, map[uint32]bool{}
	for _, c := range cats {
		if ch := s.state.Channels[c.Channel]; c.Channel != 0 && ch != nil && ch.Parent == parent.ID {
			keepCat[c.Channel] = true
		}
	}
	for _, w := range want {
		if w.Channel != 0 && w.Channel != parent.ID && !keepCat[w.Channel] && s.state.mirror(w.Channel) {
			keep[w.Channel] = true
		}
	}
	move := func(id, to uint32) {
		if err := s.state.EditChannel(id, ChannelEdit{Parent: &to}); err != nil {
			slog.Error("move a text channel mirror", "channel", id, "err", err)
			return
		}
		touched[id], inherited = true, true
	}
	// Kept mirrors leave categories that go (clients learn it before the
	// removal, or they drop the mirror with its users); then unwanted
	// channels go, so their names are free. A mirror that could not move
	// goes with its category and is made anew.
	for _, id := range slices.Sorted(maps.Keys(keep)) {
		if p := s.state.Channels[id].Parent; p != parent.ID && !keepCat[p] {
			move(id, parent.ID)
			s.showChannelLocked(s.state.Channels[id])
		}
	}
	for _, ch := range s.state.Tree() {
		if s.state.Channels[ch.ID] != nil && !keep[ch.ID] && !keepCat[ch.ID] && (ch.Parent == parent.ID || keepCat[ch.Parent]) {
			removed = append(removed, s.removeChannelLocked(ch.ID, 0)...)
		}
	}
	maps.DeleteFunc(keep, func(id uint32, _ bool) bool { return s.state.Channels[id] == nil })
	// Positions count per parent; under the parent the categories come
	// after the mirrors.
	next := map[uint32]int32{}
	position := func(parent uint32) int32 {
		next[parent]++
		return next[parent] - 1
	}
	uncategorized := int32(0)
	for _, w := range want {
		if w.Category == 0 {
			uncategorized++
		}
	}
	edit := func(ch *Channel, name, desc string, pos int32) {
		if ch.Name != name || ch.Position != pos || ch.Description != desc {
			if err := s.state.EditChannel(ch.ID, ChannelEdit{Name: &name, Position: &pos, Description: &desc}); err != nil {
				slog.Error("text channel mirror", "name", name, "err", err)
			}
			touched[ch.ID] = true
		}
	}
	catIDs = make([]uint32, len(cats))
	for i, c := range cats {
		ch := s.state.Channels[c.Channel]
		if !keepCat[c.Channel] {
			if ch, err = s.state.AddChannel(parent.ID, c.Name, false); err != nil {
				slog.Error("text category", "name", c.Name, "err", err)
				continue
			}
			touched[ch.ID] = true
		}
		if !slices.Contains(ch.ACLs, categoryACL) {
			ch.ACLs = append([]ACL{categoryACL}, ch.ACLs...)
			touched[ch.ID], inherited = true, true
		}
		edit(ch, c.Name, ch.Description, uncategorized+int32(i))
		catIDs[i] = ch.ID
	}
	ids = make([]uint32, len(want))
	for i, w := range want {
		at := parent.ID
		if w.Category > 0 && w.Category <= len(cats) && catIDs[w.Category-1] != 0 {
			at = catIDs[w.Category-1]
		}
		name := "#" + w.Name
		ch := s.state.Channels[w.Channel]
		if !keep[w.Channel] {
			if ch, err = s.state.AddChannel(at, name, false); err != nil {
				slog.Error("text channel mirror", "name", name, "err", err)
				continue
			}
			touched[ch.ID] = true // new, even if it already matches
			if w.Private {
				s.makePrivate(ch, nil)
			}
		} else if ch.Parent != at {
			move(ch.ID, at)
		}
		edit(ch, name, html.EscapeString(w.Topic), position(ch.Parent))
		ids[i] = ch.ID
	}
	// Private mirrors and categories: members groups inherit only in a
	// category (see inheritsMembers; also after a move, and for older ones).
	for _, ch := range s.state.Channels {
		g := ch.Groups[membersGroup]
		if g == nil || !isPrivate(ch) || ch.Parent != parent.ID && !s.isCategory(s.state.Channels[ch.Parent]) {
			continue
		}
		if want := s.inheritsMembers(ch); g.Inherit != want {
			g.Inherit = want
			touched[ch.ID], inherited = true, true
		}
	}
	for _, ch := range s.state.Tree() { // parents first, for clients
		if touched[ch.ID] {
			save = append(save, cloneChannel(ch))
			s.showChannelLocked(ch)
		}
	}
	if touched[parent.ID] || inherited {
		s.permissionsChangedLocked(parent) // suppress states follow Speak
	}
	lastID := s.state.LastChannelID()
	s.mu.Unlock()
	s.save(0, save, removed, lastID)
	if parent.ID != saved {
		if err := s.storeTextParent(parent.ID); err != nil {
			return catIDs, ids, err
		}
	}
	return catIDs, ids, nil
}

// mirrorACL: in the parent and the mirrors nobody speaks, edits or builds;
// texts only in the mirrors.
func mirrorACL() []ACL {
	return []ACL{
		{ApplyHere: true, ApplySubs: true, Group: "all", Deny: mirrorDeny},
		{ApplyHere: true, ApplySubs: false, Group: "all", Deny: TextMessage},
	}
}

// isCategory: a category's channel (under the mirrors' parent, with its
// entry). Under mu.
func (s *Server) isCategory(ch *Channel) bool {
	return ch != nil && s.textParent != 0 && ch.Parent == s.textParent && slices.Contains(ch.ACLs, categoryACL)
}

// categoryACL: no texts in a category itself (its other entries, such as
// its moderators', stay).
var categoryACL = ACL{ApplyHere: true, ApplySubs: false, Group: "all", Deny: TextMessage}

// inTextTree: the mirrors' parent or below it (categories, mirrors, and
// whatever SuperUser made there), at any depth: no voice channel rules
// apply there (State.Mirror). Under mu.
func (s *Server) inTextTree(channel uint32) bool {
	return s.textParent != 0 && slices.ContainsFunc(s.state.chain(channel), func(c *Channel) bool { return c.ID == s.textParent })
}

func (s *Server) loadTextParent() (uint32, error) {
	if s.cfg.DB == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.textParent, nil
	}
	return s.cfg.DB.TextParent()
}

func (s *Server) storeTextParent(id uint32) error {
	if s.cfg.DB == nil {
		return nil // s.textParent has it
	}
	return s.cfg.DB.SetTextParent(id)
}

// SendServerText sends HTML to one user, from the server (no actor).
// MumbleUser is who a Mumble client is (see MumbleClient).
type MumbleUser struct {
	Registered bool
	UserID     uint32 // with Registered
}

// MumbleClient is who the session is (also a web session's), and whether
// it is a Mumble client (not a web client through the bridge).
func (s *Server) MumbleClient(session uint32) (MumbleUser, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, u := s.clients[session], s.state.Users[session]
	if c == nil || u == nil {
		return MumbleUser{}, false
	}
	return MumbleUser{Registered: u.Registered, UserID: u.UserID}, !c.web
}

func (s *Server) SendServerText(session uint32, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.clients[session]; c != nil {
		c.sendMsg(mumble.TypeTextMessage, &pb.TextMessage{Session: []uint32{session}, Message: proto.String(text)})
	}
}

// eventQueue runs hooks on one goroutine, in order, outside mu. It has no
// bound: joins, moves and leaves must not get lost (they come from
// clients' messages, which are rate-limited).
type eventQueue struct {
	mu      sync.Mutex
	pending []func()
	closed  bool
	stopped bool // the last hooks were taken: later ones are lost
	wake    chan struct{}
	done    chan struct{} // the goroutine ended
}

// A queue this long means hooks are slow or stuck: a warning.
const longEventQueue = 10000

func newEventQueue() *eventQueue {
	q := &eventQueue{wake: make(chan struct{}, 1), done: make(chan struct{})}
	go q.run()
	return q
}

// add never blocks (it runs under mu).
func (q *eventQueue) add(f func()) {
	q.mu.Lock()
	stopped := q.stopped
	if !stopped {
		q.pending = append(q.pending, f)
	}
	n := len(q.pending)
	q.mu.Unlock()
	switch {
	case stopped:
		slog.Warn("an event came after shutdown: it was lost")
	case n == longEventQueue:
		slog.Warn("the event queue is long: hooks are slow", "events", n)
	}
	q.signal()
}

func (q *eventQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *eventQueue) run() {
	defer close(q.done)
	for range q.wake {
		q.mu.Lock()
		batch, closed := q.pending, q.closed
		q.pending, q.stopped = nil, closed
		q.mu.Unlock()
		for _, f := range batch {
			f()
		}
		if closed {
			return
		}
	}
}

// close runs the hooks queued so far and stops; it waits at most d.
func (q *eventQueue) close(d time.Duration) {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
	select {
	case <-q.done:
	case <-time.After(d):
	}
}
