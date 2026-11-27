package mumbled

import (
	"crypto/sha1" // #nosec G505 -- the Mumble protocol's blob hash
	"log/slog"

	"google.golang.org/protobuf/proto"

	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/mumble/pb"
)

const (
	// As Murmur: from this size, clients get a hash and ask for the data.
	blobHashMin = 128
	maxTexture  = 120 << 10
	maxBlobAsks = 100 // per RequestBlob
)

// blobHash is the SHA-1 that stands for long data, or nil for short data.
func blobHash(b []byte) []byte {
	if len(b) < blobHashMin {
		return nil
	}
	h := sha1.Sum(b) // #nosec G401 -- the Mumble protocol's blob hash
	return h[:]
}

// inlineOrHash: short data goes out as it is, long data as its hash.
func inlineOrHash(b []byte) (inline, hash []byte) {
	if h := blobHash(b); h != nil {
		return nil, h
	}
	return b, nil
}

func commentFields(comment string) (*string, []byte) {
	inline, hash := inlineOrHash([]byte(comment))
	if hash != nil {
		return nil, hash
	}
	return proto.String(string(inline)), nil
}

// setUserBlobs puts a user's comment and avatar into a UserState.
func setUserBlobs(m *pb.UserState, u *User) {
	if u.Comment != "" {
		m.Comment, m.CommentHash = commentFields(u.Comment)
	}
	if len(u.Texture) > 0 {
		m.Texture, m.TextureHash = inlineOrHash(u.Texture)
	}
}

// contentChange checks a comment or avatar change as Murmur: anyone for
// itself; for others only clearing, with ResetUserContent in root. Not as
// Murmur: guests may not set an avatar (they would lose it when they leave).
// The comment is plain text already (see userStateMsg).
func (s *Server) contentChange(actor, u *User, m *pb.UserState) (Perm, error) {
	if u == actor && !u.Registered && len(m.Texture) > 0 {
		return 0, deny(pb.PermissionDenied_Text, "Register to set a profile picture.")
	}
	if u != actor {
		if !s.state.Has(actor, 0, ResetUserContent) {
			return ResetUserContent, nil
		}
		if m.GetComment() != "" || len(m.Texture) > 0 {
			return 0, deny(pb.PermissionDenied_TextTooLong, "others' comments and avatars can only be cleared")
		}
	}
	if len(m.GetComment()) > maxTextLength || len(m.Texture) > maxTexture {
		return 0, deny(pb.PermissionDenied_TextTooLong, "too long")
	}
	return 0, nil
}

// applyContent sets a user's comment or avatar and the fields for others
// (a cleared one goes out empty).
func applyContent(u *User, m, out *pb.UserState) {
	if m.Comment != nil {
		u.Comment = m.GetComment()
		out.Comment, out.CommentHash = commentFields(u.Comment)
	}
	if m.Texture != nil {
		u.Texture = m.Texture
		out.Texture, out.TextureHash = inlineOrHash(u.Texture)
		if out.Texture == nil && out.TextureHash == nil {
			out.Texture = []byte{}
		}
	}
}

// content is what is stored of a registered user's profile.
type content struct {
	registered bool
	userID     uint32
	comment    string
	texture    []byte
	newTexture bool // the avatar changed (see OnPictureChanged)
}

func contentOf(u *User, newTexture bool) content {
	return content{u.Registered, u.UserID, u.Comment, u.Texture, newTexture}
}

// saveContent stores a registered user's comment and avatar. Callers hold
// saveMu, so saves keep the order of the changes.
func (s *Server) saveContent(c content) {
	if !c.registered || s.cfg.DB == nil {
		return
	}
	if err := s.cfg.DB.SetUserContent(c.userID, c.comment, c.texture); err != nil {
		slog.Error("saving a comment", "user", c.userID, "err", err)
		return
	}
	if f := s.cfg.OnPictureChanged; f != nil && c.newTexture {
		picture := avatarType(c.texture) != ""
		s.events.add(func() { f(c.userID, picture) })
	}
}

// requestBlobMsg sends the full comments, avatars and descriptions that
// went out as hashes.
// Each id is answered once (as Murmur); the data is copied under the lock
// (it is never changed in place) and marshalled after it.
func (s *Server) requestBlobMsg(c *client, m *pb.RequestBlob) {
	var replies []proto.Message
	var types []uint16
	reply := func(typ uint16, msg proto.Message) {
		types, replies = append(types, typ), append(replies, msg)
	}
	s.mu.Lock()
	for _, id := range unique(m.SessionTexture) {
		if u := s.state.Users[id]; u != nil && len(u.Texture) > 0 && c.sees(u) {
			reply(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(id), Texture: u.Texture})
		}
	}
	for _, id := range unique(m.SessionComment) {
		if u := s.state.Users[id]; u != nil && u.Comment != "" && c.sees(u) {
			reply(mumble.TypeUserState, &pb.UserState{Session: proto.Uint32(id), Comment: proto.String(u.Comment)})
		}
	}
	for _, id := range unique(m.ChannelDescription) {
		if ch := s.state.Channels[id]; ch != nil && ch.Description != "" && c.seen[id] {
			reply(mumble.TypeChannelState, &pb.ChannelState{ChannelId: proto.Uint32(id), Description: proto.String(ch.Description)})
		}
	}
	s.mu.Unlock()
	for i, msg := range replies {
		c.sendMsg(types[i], msg)
	}
}

// unique keeps the first maxBlobAsks distinct ids.
func unique(ids []uint32) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for _, id := range ids {
		if !seen[id] && len(out) < maxBlobAsks {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
