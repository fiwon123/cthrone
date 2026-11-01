//go:build gui

package ui

import (
	"sync"
	"testing"
	"time"

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
