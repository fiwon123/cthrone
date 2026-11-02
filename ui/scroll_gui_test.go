//go:build gui

package ui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"
)

// withOffset puts the transcript where a reader would be, without a rendered
// list deciding it.
func withOffset(t *testing.T, offset float32) {
	t.Helper()

	previous := chatOffset
	chatOffset = func(*widget.List) float32 { return offset }

	t.Cleanup(func() { chatOffset = previous })
}

// Reading at the bottom: a new message follows, as it always did.
func TestANewMessageFollowsWhenTheReaderIsAtTheBottom(t *testing.T) {
	g := newScreenGUI(t, false)
	g.lastBottom = 100

	withOffset(t, 100)

	g.addMessage(chatMessage{from: "alice", text: "hello"})

	if g.jumpBtn.Visible() {
		t.Error("the way down is offered although the reader is at the end")
	}
}

// Scrolled up: the view stays where the reader put it, and the way back down
// appears (#434).
func TestANewMessageDoesNotMoveAReaderWhoIsScrolledUp(t *testing.T) {
	g := newScreenGUI(t, false)
	g.lastBottom = 400

	withOffset(t, 0)

	g.addMessage(chatMessage{from: "alice", text: "hello"})

	if !g.jumpBtn.Visible() {
		t.Error("no way down was offered to a reader who is scrolled up")
	}
}

// The way down takes the transcript to its end and goes away again.
func TestTheWayDownReturnsToTheEnd(t *testing.T) {
	g := newScreenGUI(t, false)
	g.lastBottom = 400
	g.jumpBtn.Show()

	withOffset(t, 0)
	g.scrollToLatest()

	if g.jumpBtn.Visible() {
		t.Error("the way down is still offered after taking it")
	}
}

// A message you typed takes you to it, wherever you were reading: you sent it,
// you are looking at it.
func TestYourOwnMessageTakesYouToIt(t *testing.T) {
	g := newScreenGUI(t, false)
	g.lastBottom = 400

	withOffset(t, 0)

	g.entry.SetText("hello there")
	g.send("hello there")

	if g.jumpBtn.Visible() {
		t.Error("sending a message left the way down on screen")
	}
}

// The perf line arrives every 25 messages and carries the average, the last
// cost, the rows and the live sessions — the numbers a report needs (#432).
func TestThePerfLineSummarisesTheMessagePath(t *testing.T) {
	g := newScreenGUI(t, false)
	g.perfEnabled = true

	g.mu.Lock()
	g.messages = make([]chatMessage, 7)
	g.mu.Unlock()

	g.liveSessions = 2

	var (
		lines []string
	)

	for i := 0; i < 25; i++ {
		if line := g.noteMessageCost(2 * time.Millisecond); line != "" {
			lines = append(lines, line)
		}
	}

	if len(lines) != 1 {
		t.Fatalf("%d perf lines after 25 messages, want 1", len(lines))
	}

	line := lines[0]
	for _, want := range []string{"25 messages", "2.00 ms each", "last 2.00 ms", "7 rows", "2 live"} {
		if !strings.Contains(line, want) {
			t.Errorf("the perf line %q does not carry %q", line, want)
		}
	}

	// And nothing is written again until the next block is complete.
	if line := g.noteMessageCost(time.Millisecond); line != "" {
		t.Errorf("a line was written before the next block: %q", line)
	}
}
