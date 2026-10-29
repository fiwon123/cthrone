package ui

import "strings"

// maxMessages bounds the transcript: a window left open for hours must not grow
// without limit. The oldest line and its row are dropped together.
const maxMessages = 500

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

// transcript renders every message in order, one per line — what "Copy all"
// puts on the clipboard.
func transcript(messages []chatMessage) string {
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.line()
	}

	return strings.Join(lines, "\n")
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
