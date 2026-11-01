//go:build gui

package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// sendRecorder is the live session the send path talks to: it records what it
// was handed and says so, so the test never waits on a socket.
type sendRecorder struct {
	*session.Base

	mu   sync.Mutex
	sent [][]byte
	done chan struct{}
}

func newSendRecorder() *sendRecorder {
	return &sendRecorder{Base: session.NewBase("alice", "alice:8080"), done: make(chan struct{}, 1)}
}

func (r *sendRecorder) Send(payload []byte) error {
	r.mu.Lock()
	r.sent = append(r.sent, append([]byte(nil), payload...))
	r.mu.Unlock()

	select {
	case r.done <- struct{}{}:
	default:
	}

	return nil
}

func (r *sendRecorder) Close(reason string) { r.NotifyClose(reason) }

// wait waits for the send path's goroutine to hand the session a payload.
func (r *sendRecorder) wait(t *testing.T) []byte {
	t.Helper()

	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the message never reached the session")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.sent[len(r.sent)-1]
}

// A line that begins with the wire magic used to be refused here with "Not
// sent", because a peer would read it as a frame. It is wrapped in a text frame
// instead, so it travels as the sentence it is.
func TestSendWrapsALineThatBeginsWithTheMagic(t *testing.T) {
	g := newTestGUI(t)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.send("CTH1 says hello")

	payload := sess.wait(t)

	if !wire.IsFrame(payload) {
		t.Fatalf("the line was sent unwrapped, so a peer would read it as a frame: %q", payload)
	}

	frame, err := wire.Parse(payload)
	if err != nil {
		t.Fatalf("the sent payload does not parse: %v", err)
	}

	if frame.Header.Kind != wire.KindText || string(frame.Payload) != "CTH1 says hello" {
		t.Fatalf("sent %q (%q), want the line as text", frame.Header.Kind, frame.Payload)
	}
}

// Ordinary text is still handed over byte for byte, so a peer that predates the
// envelope reads it as it always did.
func TestSendLeavesOrdinaryTextAlone(t *testing.T) {
	g := newTestGUI(t)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.send("hello")

	if got := string(sess.wait(t)); got != "hello" {
		t.Fatalf("sent %q, want the text unchanged", got)
	}
}

// What a peer had to wrap because the line begins with the magic is shown as
// the line: the transcript says what the sender typed, not that a transfer
// arrived damaged.
func TestReceiveShowsWrappedTextAsTheLine(t *testing.T) {
	g := newTestGUI(t)

	payload, err := wire.Encode(wire.Header{Kind: wire.KindText}, []byte("CTH1 says hello"))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	g.receiveFrame("alice", payload)

	lines := waitForMessages(t, g, 1)

	if got := lines[0].text; got != "CTH1 says hello" {
		t.Fatalf("the transcript shows %q, want the line", got)
	}

	if got := lines[0].from; got != "alice" {
		t.Errorf("sender = %q, want alice", got)
	}
}

// waitForMessages waits for the transcript to reach n rows and returns them.
// addMessage posts to the UI goroutine, so a test cannot read the log straight
// after the call.
func waitForMessages(t *testing.T, g *gui, n int) []chatMessage {
	t.Helper()

	for i := 0; i < 400; i++ {
		g.mu.Lock()
		lines := append([]chatMessage(nil), g.messages...)
		g.mu.Unlock()

		if len(lines) >= n {
			return lines
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("the transcript never reached %d rows", n)

	return nil
}

// The composer is a chat field rather than a form field: Enter starts a new
// line, Shift+Enter sends the whole body as one message, and the Send button is
// the third way in — the only one a phone has, since its soft keyboard turns
// the action key into a newline.
//
// The assertion that matters is the negative one: a finished line followed by
// Enter must not send, because that is what a message typed in two parts used
// to do.
func TestComposerStartsANewLineAndSendsOnShiftEnter(t *testing.T) {
	g := newTestGUI(t)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	if !g.entry.MultiLine {
		t.Fatal("the composer is single-line, so a message can never be more than one line")
	}

	typeRunes(g.entry, "first line")
	g.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	typeRunes(g.entry, "second line")

	if got, want := g.entry.Text, "first line\nsecond line"; got != want {
		t.Fatalf("after Enter the field reads %q, want %q", got, want)
	}

	select {
	case <-sess.done:
		t.Fatal("Enter sent the message, want a new line")
	default:
	}

	g.entry.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	g.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	g.entry.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})

	// Ordinary text is handed over byte for byte — the envelope only wraps a
	// line that begins with the magic — so the newline is what the peer gets.
	if got, want := string(sess.wait(t)), "first line\nsecond line"; got != want {
		t.Fatalf("sent %q, want %q", got, want)
	}

	if got := g.entry.Text; got != "" {
		t.Errorf("the field still reads %q after sending, want it cleared", got)
	}
}

// A long message must not grow the composer: the text scrolls inside the field
// rather than pushing the transcript off the screen. Fyne sizes a multi-line
// entry from its visible-row count, so the height the field reports is the same
// before and after a paragraph is typed into it.
func TestComposerKeepsItsHeightWhateverIsTyped(t *testing.T) {
	g := newTestGUI(t)

	before := test.WidgetRenderer(g.entry).MinSize().Height

	g.entry.SetText(strings.Repeat("a line of a much longer message\n", 20))

	after := test.WidgetRenderer(g.entry).MinSize().Height

	if after > before {
		t.Fatalf("the composer grew from %.1f to %.1f for a longer message, want a fixed height", before, after)
	}
}

// typeRunes types one rune at a time, so the cursor follows the text the way a
// keyboard leaves it — SetText would leave the cursor where it was.
func typeRunes(entry *widget.Entry, text string) {
	for _, r := range text {
		entry.TypedRune(r)
	}
}

// A multi-line message is trimmed at its ends and nowhere else: the line breaks
// a user typed inside it are part of the message.
func TestSendKeepsInnerNewlinesAndTrimsTheEnds(t *testing.T) {
	g := newTestGUI(t)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.send("\n  first paragraph\n\nsecond paragraph  \n")

	if got, want := string(sess.wait(t)), "first paragraph\n\nsecond paragraph"; got != want {
		t.Fatalf("sent %q, want %q", got, want)
	}
}

// A message of nothing but whitespace is not a message: the line breaks Enter
// now types would otherwise be enough to send an empty line to the room.
func TestSendRefusesAMessageOfOnlyWhitespace(t *testing.T) {
	g := newTestGUI(t)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.entry.SetText(" \n\t\n ")

	g.send(g.entry.Text)

	select {
	case <-sess.done:
		t.Fatal("whitespace-only text reached the session, want nothing sent")
	case <-time.After(20 * time.Millisecond):
	}

	g.mu.Lock()
	messages := len(g.messages)
	g.mu.Unlock()

	if messages != 0 {
		t.Errorf("the transcript has %d rows after a whitespace-only send, want none", messages)
	}
}

// The transcript shows the line breaks: a row is a wrapping label whose item
// height is taken from its minimum, so a message that is three lines tall has
// to report a taller row than a one-line message at the same width — otherwise
// the list clips it to its first line.
func TestChatRowGrowsForAMultiLineMessage(t *testing.T) {
	g := newTestGUI(t)

	g.mu.Lock()
	g.messages = []chatMessage{
		{text: "one line"},
		{text: "first line\nsecond line\nthird line"},
	}
	g.mu.Unlock()

	height := func(id int) float32 {
		row := g.chatList.CreateItem()
		row.Resize(fyne.NewSize(400, row.MinSize().Height))
		g.updateChatRow(id, row)

		return row.MinSize().Height
	}

	one, many := height(0), height(1)

	if many <= one {
		t.Fatalf("a three-line message reports %.1f and a one-line message %.1f, want the taller row to show its lines", many, one)
	}
}
