package ui

import (
	"strings"
	"sync"
)

// maxMessages bounds the transcript: a window left open for hours must not grow
// without limit. The oldest line and its row are dropped together.
const maxMessages = 500

// composerRowsDesktop and composerRowsTouch are how many lines the message
// field shows before the text scrolls inside it. A phone gets one row fewer:
// the transcript above it has less room to spare, and the Send button sits
// under the field rather than beside it.
const (
	composerRowsDesktop = 4
	composerRowsTouch   = 3
)

// composerRowCount is the visible-row count for the layout in force.
//
// It is a ceiling, not a starting size: Fyne sizes a multi-line entry from its
// row count rather than from the text in it, so the composer is the same height
// whether it holds one line or twenty. Growing with the text would mean a
// scroll wrapper with a height recomputed on every keystroke, and this layout
// has been broken by height arithmetic before (#186, #189).
func composerRowCount(stack bool) int {
	if stack {
		return composerRowsTouch
	}

	return composerRowsDesktop
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

// transcript renders every message in order, one per line — what "Copy all"
// puts on the clipboard.
func transcript(messages []chatMessage) string {
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.line()
	}

	return strings.Join(lines, "\n")
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
