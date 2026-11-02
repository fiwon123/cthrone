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

// composerHint is the message field's placeholder. The two platforms differ in
// exactly one thing — how a message is sent — so the hint names that, and both
// say that Enter starts a new line: a user who expects Enter to send would
// otherwise think the field was broken.
//
// The phone has no Shift+Enter to press — its soft keyboard turns the action
// key into a newline for a multi-line field — so the button is what the touch
// hint names.
func composerHint(stack bool) string {
	if stack {
		return "Type a message… Enter starts a new line, Send sends it"
	}

	return "Type a message… Enter starts a new line, Shift+Enter sends it"
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
