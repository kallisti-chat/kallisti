package sidecar

// MayReadUpload is the experimental private-room media gate for a file
// link without a signature: the readers of the file's chat, as for pushes.
// user is who asks (false: nobody logged in), asked only when a guest may
// not read. Direct chats keep their files public; a file not sent yet has
// no chat and is nobody's.
func (h *Hub) MayReadUpload(key string, user func() (uint32, bool)) bool {
	channel, err := h.store.UploadChannel(key)
	if err != nil {
		return false
	}
	c, ok := h.chatOf(channel)
	switch {
	case !ok:
		return false
	case c.direct, h.offlineMay(nil, c):
		return true
	}
	id, ok := user()
	return ok && h.offlineMay(&id, c)
}
