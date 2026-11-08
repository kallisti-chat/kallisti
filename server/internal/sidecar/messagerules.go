package sidecar

import (
	"errors"
	"strings"
	"unicode/utf8"

	"kallisti.chat/server/internal/homes"
	"kallisti.chat/server/internal/msgrules"
	"kallisti.chat/server/internal/mumble"
	"kallisti.chat/server/internal/store"
)

// Message rules: the built-in server checks sends, edits and uploads (see
// Voice.CheckMessage; it also checks its Mumble clients' texts), and
// replaces words in their texts. Without it (Murmur) there are none.

// refused: the rules refuse a send, an edit or an upload; the author sees
// why. Else req has the text to post. Sends are accounted in post only
// after their content is valid.
func (c *Conn) refused(id mumble.Identity, req *request) bool {
	var m msgrules.Message
	field := &req.Text
	switch req.Type {
	case "send":
		m = msgrules.Message{Text: req.Text, Upload: len(req.Uploads) > 0, MentionAll: mentionAllOf(req.Text) != ""}
	case "edit-message":
		m = msgrules.Message{Text: req.Text, MentionAll: mentionAllOf(req.Text) != ""}
	case "upload-token":
		m = msgrules.Message{Upload: true}
	case "create-thread", "rename-thread": // thread names; a new thread is a post
		m, field = msgrules.Message{Text: req.Name, Post: req.Type == "create-thread"}, &req.Name
	default:
		return false
	}
	if c.hub.voice == nil {
		return false
	}
	text, err := c.hub.voice.CheckMessage(id.Session, m)
	if err == nil && text != *field && utf8.RuneCountInString(strings.TrimSpace(text)) > MaxTextLength {
		err = errors.New("This message is too long with the word replacements")
	}
	if err != nil {
		if req.Type == "send" { // the text comes back for the composer
			c.send(map[string]any{"type": "error", "message": err.Error(), "about": "refused", "channel": req.Channel, "text": req.composerText()})
		} else {
			c.sendError(err.Error())
		}
		return true
	}
	*field = text
	if req.poll != nil { // the replacements in the poll itself too
		c.replaceWords(id, req.poll)
		req.Text = pollText(*req.poll)
	}
	return false
}

// replaceWords applies the rules' word replacements to a poll's question
// and answers (the whole poll passed the rules).
func (c *Conn) replaceWords(id mumble.Identity, p *store.Poll) {
	clean := func(s string) string {
		if text, err := c.hub.voice.CheckMessage(id.Session, msgrules.Message{Text: s}); err == nil && strings.TrimSpace(text) != "" {
			return oneLine(text)
		}
		return s
	}
	p.Question = clean(p.Question)
	for i := range p.Answers {
		p.Answers[i].Text = clean(p.Answers[i].Text)
	}
}

// messageRules answers an admin: the rules, or new ones saved.
func (c *Conn) messageRules(id mumble.Identity, req request) {
	if c.hub.voice == nil {
		c.manageError("message rules need the built-in Mumble server")
		return
	}
	var r msgrules.Rules
	var err error
	if req.Type == "set-message-rules" && req.Rules != nil {
		r, err = c.hub.voice.SetMessageRules(id.Session, *req.Rules)
	} else {
		r, err = c.hub.voice.MessageRules(id.Session)
	}
	if err != nil {
		c.manageError(err.Error())
		return
	}
	if r.Words == nil { // empty lists, not null: the client wants lists
		r.Words = []string{}
	}
	if r.Domains == nil {
		r.Domains = []string{}
	}
	if r.Replacements == nil {
		r.Replacements = []msgrules.Replacement{}
	}
	c.send(map[string]any{"type": "message-rules", "rules": r})
}

// homeServers answers an admin: which other servers' accounts log in here,
// or new lists saved.
func (c *Conn) homeServers(id mumble.Identity, req request) {
	if c.hub.voice == nil {
		c.manageError("this setting needs the built-in Mumble server")
		return
	}
	var r homes.Rules
	var err error
	if req.Type == "set-home-servers" && req.Homes != nil {
		r, err = c.hub.voice.SetHomeServers(id.Session, *req.Homes)
	} else {
		r, err = c.hub.voice.HomeServers(id.Session)
	}
	if err != nil {
		c.manageError(err.Error())
		return
	}
	if req.Type == "set-home-servers" && req.Homes != nil && c.hub.OnHomesChanged != nil {
		c.hub.OnHomesChanged()
	}
	if r.Hosts == nil { // a list, not null
		r.Hosts = []string{}
	}
	if r.Mode == "" {
		r.Mode = "all"
	}
	c.send(map[string]any{"type": "home-servers", "homes": r})
}
