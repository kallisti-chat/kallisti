package sidecar

// MayReadUpload applies the experimental room-only media policy to an
// attached upload. The callback checks the current Mumble channel access;
// threads inherit their parent's access. Direct messages are deliberately
// outside this experiment and retain their existing media behavior.
//
// Unattached uploads are never served while the experiment is enabled:
// their destination is not known yet. The composer uses local previews.
// Missing/deleted messages, threads and mirrors fail closed.
func (h *Hub) MayReadUpload(key string, room func(channel uint32, text bool) bool) bool {
	m, err := h.store.MessageByUpload(key)
	if err != nil {
		return false
	}
	c, ok := h.chatOf(m.ChannelID)
	if !ok {
		return false
	}
	if c.direct {
		return true
	}
	c, ok = h.parentOf(c)
	if !ok {
		return false
	}
	if c.isVoice {
		return room(c.voice, false)
	}
	mirror, ok := h.textMirror(c.id)
	return ok && mirror != 0 && room(mirror, true)
}
