package doc

// Entry is one place in the navigation history.
type Entry struct {
	Path     string
	Fragment string
	Scroll   float64 // scroll offset when the user left the entry
}

// History is a browser-style back/forward list. The zero value is empty.
type History struct {
	entries []Entry
	pos     int // index of the current entry; meaningless when empty
}

// Current returns the current entry, if any.
func (h *History) Current() (Entry, bool) {
	if len(h.entries) == 0 {
		return Entry{}, false
	}
	return h.entries[h.pos], true
}

// SetScroll records the scroll offset of the current entry.
func (h *History) SetScroll(y float64) {
	if len(h.entries) > 0 {
		h.entries[h.pos].Scroll = y
	}
}

// Visit adds e after the current entry and drops the forward entries.
func (h *History) Visit(e Entry) {
	if len(h.entries) > 0 {
		h.entries = h.entries[:h.pos+1]
	}
	h.entries = append(h.entries, e)
	h.pos = len(h.entries) - 1
}

// Back moves to the previous entry.
func (h *History) Back() (Entry, bool) {
	if !h.CanBack() {
		return Entry{}, false
	}
	h.pos--
	return h.entries[h.pos], true
}

// Forward moves to the next entry.
func (h *History) Forward() (Entry, bool) {
	if !h.CanForward() {
		return Entry{}, false
	}
	h.pos++
	return h.entries[h.pos], true
}

// CanBack reports whether there is a previous entry.
func (h *History) CanBack() bool { return len(h.entries) > 0 && h.pos > 0 }

// CanForward reports whether there is a next entry.
func (h *History) CanForward() bool { return h.pos < len(h.entries)-1 }

// Peek returns the entry delta steps from the current one without moving.
func (h *History) Peek(delta int) (Entry, bool) {
	i := h.pos + delta
	if len(h.entries) == 0 || i < 0 || i >= len(h.entries) {
		return Entry{}, false
	}
	return h.entries[i], true
}

// Move moves delta steps, if that entry exists.
func (h *History) Move(delta int) {
	if _, ok := h.Peek(delta); ok {
		h.pos += delta
	}
}
