package ui

import (
	"strings"
	"sync"
)

// maxMessages bounds the transcript: a window left open for hours must not grow
// without limit. The oldest line and its row are dropped together.
const maxMessages = 500

// composerRowsMax is the most lines the message field shows before the text
// scrolls inside it. One line at rest, growing with the message, is the shape a
// chat app teaches (#324); a fixed four rows wasted the transcript's height on an
// empty field and stopped a long message short.
const composerRowsMax = 5

// composerDefaultColumns is the estimate used before the field has been measured
// — a phone at rest, which is the narrow case.
const composerDefaultColumns = 32

// composerRows is how many rows the field should show for text in a field of
// columns characters: one per new line, plus one for each wrap, capped.
//
// Fyne wraps by pixel width and offers no measurement for "how many lines is
// this", so the width is turned into columns by the caller and this counts
// characters. It errs towards growing a line early, which is the harmless
// direction: the field never hides a line the user is typing.
func composerRows(text string, columns int) int {
	if columns <= 0 {
		columns = composerDefaultColumns
	}

	rows := 0

	for _, line := range strings.Split(text, "\n") {
		wrapped := (len([]rune(line)) + columns - 1) / columns
		if wrapped < 1 {
			wrapped = 1
		}

		rows += wrapped
	}

	if rows < 1 {
		rows = 1
	}

	if rows > composerRowsMax {
		rows = composerRowsMax
	}

	return rows
}

// composerColumns is how many characters fit in a field of width pixels.
//
// It is an estimate — the theme's text size plus a little air — because the
// alternative is measuring the text, and that is what the transcript's height
// arithmetic got wrong twice (#186, #189).
func composerColumns(width float32) int {
	if width <= 0 {
		return composerDefaultColumns
	}

	return int(width / 8)
}

// chatMessage is one line of the transcript: what was said, and who said it. A
// message the user typed themselves carries no sender and renders as "You".
//
// A row can also report a file transfer instead of a sentence: file is then set
// and text is empty, because a transfer says what it is doing from its own
// state, not from prose.
type chatMessage struct {
	from string
	text string
	file *fileNote

	// hidden marks a message the sender asked to keep masked until it is
	// revealed, for a secret typed into a room (#469). revealed is this
	// window's answer to that: it is not sent, so a line revealed here is
	// revealed only here.
	hidden   bool
	revealed bool
}

// hiddenBody is what a masked row draws instead of its text.
//
// A fixed run, not one dot per character: the length of a password is not the
// room's business either, and a long message would otherwise become a row of
// dots the height of the screen.
const hiddenBody = "••••••••"

// masked reports whether this window is hiding the body at the moment.
func (m chatMessage) masked() bool {
	return m.hidden && !m.revealed
}

// line renders the message the way the transcript shows it.
func (m chatMessage) line() string {
	sender := "You"
	if m.from != "" {
		sender = m.from
	}

	if m.file != nil {
		return sender + ": " + m.file.line()
	}

	return sender + ": " + m.text
}

// body is what a message says without the sender in front of it.
//
// The row draws the sender on a line of its own, in its own colour (#346), so the
// caption carries the rest; line() keeps the whole thing for the clipboard, where
// a bare message would lose who said it. A hidden message draws the mask here
// and its real text in line(): revealing is what the row is for, copying is
// what the clipboard is for (#469).
func (m chatMessage) body() string {
	if m.file != nil {
		return m.file.line()
	}

	if m.masked() {
		return hiddenBody
	}

	return m.text
}

// senderDisplay is the name a row shows for its sender: "You" for what this
// device said, and the authenticated name otherwise.
func (m chatMessage) senderDisplay() string {
	if m.from == "" {
		return "You"
	}

	return m.from
}

// senderLabel is how an arriving message is attributed in the transcript: the
// device name, with the address it came from when the transport knows one.
//
// Both are worth showing, for the reason the terminal shows both: the name is
// who the handshake authenticated, and does not say which machine on the
// network it is; two devices can share a hostname. A bus has no per-peer
// address, so the name stands alone. An unnamed peer is still labelled,
// because the one thing it must never render as is "You".
func senderLabel(from, addr string) string {
	if from == "" {
		from = "peer"
	}

	if addr == "" || addr == from {
		return from
	}

	return from + " (" + addr + ")"
}

// peerName is the display name a close notice reports: the peer the sealed
// handshake authenticated, else the last sender, else "peer".
//
// The transport's callbacks write it and the close callback reads it, so every
// access holds the lock.
type peerName struct {
	mu   sync.Mutex
	name string
}

// set records the name the handshake authenticated. It is authoritative: a
// later frame carries a sender string too, and that one must not rename a
// peer the handshake already named.
func (p *peerName) set(name string) {
	if name == "" {
		return
	}

	p.mu.Lock()
	p.name = name
	p.mu.Unlock()
}

// learn records a sender from a message, and is used only until the handshake
// names the peer: a session that never reports a peer still has a name to
// report when it closes.
func (p *peerName) learn(name string) {
	if name == "" {
		return
	}

	p.mu.Lock()
	if p.name == "" {
		p.name = name
	}
	p.mu.Unlock()
}

// get is the name to print, or "peer" when nothing named it.
func (p *peerName) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.name == "" {
		return "peer"
	}

	return p.name
}

// disconnectLine is the transcript line a session's close leaves behind: who
// left, and why. The status line carries the reason too, but the transcript is
// the record a host reads after the room is empty.
func disconnectLine(peer, reason string) string {
	if peer == "" {
		peer = "peer"
	}

	if reason == "" {
		return peer + " disconnected"
	}

	return peer + " disconnected: " + reason
}

// appendMessage adds m to the log and drops the oldest line beyond maxMessages.
//
// The log is the view's only source of truth — the transcript reads it by
// index — so one Refresh after this call keeps the rows and the messages the
// same length, with no second place to update.
func appendMessage(messages []chatMessage, m chatMessage) []chatMessage {
	messages = append(messages, m)
	if len(messages) <= maxMessages {
		return messages
	}

	// Copy down rather than re-slicing: the log keeps its buffer instead of
	// walking forward through it, so it neither grows nor leaks what it dropped.
	copy(messages, messages[len(messages)-maxMessages:])

	return messages[:maxMessages]
}

// noSelection is the index a selection has not been given yet. Zero would be a
// perfectly good message, so the empty state cannot be the zero value (#469).
const noSelection = -1

// selection is the span of transcript rows a reader is building.
//
// It replaces drag-select handles for the copy gesture: the first tap marks one
// end and the next marks the other, which is the gesture a thumb can make on a
// long, wrapping message (#469). anchor is the first message tapped and focus
// the last, and either may come first.
type selection struct {
	active bool
	anchor int
	focus  int
}

// start is the lower index of the span, or noSelection before the first tap.
func (s selection) start() int {
	if s.anchor < 0 || s.focus < 0 {
		return noSelection
	}

	if s.anchor < s.focus {
		return s.anchor
	}

	return s.focus
}

// end is the higher index of the span, or noSelection before the first tap.
func (s selection) end() int {
	if s.anchor < 0 || s.focus < 0 {
		return noSelection
	}

	if s.anchor > s.focus {
		return s.anchor
	}

	return s.focus
}

// count is how many rows the span covers.
func (s selection) count() int {
	if !s.active {
		return 0
	}

	start := s.start()
	if start == noSelection {
		return 0
	}

	return s.end() - start + 1
}

// contains reports whether a row index is inside the span, which is what the
// row painter marks.
func (s selection) contains(index int) bool {
	if !s.active {
		return false
	}

	return s.start() != noSelection && index >= s.start() && index <= s.end()
}

// transcriptOf renders messages in order, one per line: the whole-log copy, and
// the range copy the selection header offers.
func transcriptOf(messages []chatMessage) string {
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.line()
	}

	return strings.Join(lines, "\n")
}

// removeRange takes a span out of the log and returns the shortened slice.
//
// It clears the tail it dropped as well: the rows may hold a file note with a
// path and a thumbnail, and a log that keeps pointing at them after a delete is
// memory held for a message nobody can reach any more.
func removeRange(messages []chatMessage, from, to int) []chatMessage {
	if from < 0 {
		from = 0
	}

	if to >= len(messages) {
		to = len(messages) - 1
	}

	if from >= len(messages) || from > to {
		return messages
	}

	out := append(messages[:from], messages[to+1:]...)
	for i := len(out); i < len(messages); i++ {
		messages[i] = chatMessage{}
	}

	return out
}
