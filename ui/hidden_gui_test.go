//go:build gui

package ui

import (
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/wire"
)

// The Hide toggle masks the composer while a secret is typed, and the next press
// shows it again (#469).
func TestHideToggleMasksTheComposer(t *testing.T) {
	g := newScreenGUI(t, false)

	if g.entry.Password {
		t.Fatal("the composer starts masked")
	}

	g.toggleHidden()

	if !g.hidden || !g.entry.Password {
		t.Error("Hide did not mask the composer")
	}

	g.toggleHidden()

	if g.hidden || g.entry.Password {
		t.Error("the second press left the composer masked")
	}
}

// A hidden row draws the mask, and the first tap reveals it rather than copying
// it. The next tap is the copy (#469).
func TestAHiddenRowIsMaskedUntilRevealed(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{text: "hunter2", hidden: true}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	label, _, _, _, _ := chatRowObjects(t, row)
	if got := label.Text(); got != hiddenBody {
		t.Fatalf("a hidden row draws %q, want the mask", got)
	}

	g.app.Clipboard().SetContent("")
	g.selectChatRow(0)

	if got := g.app.Clipboard().Content(); got != "" {
		t.Errorf("the first tap copied %q, want only a reveal", got)
	}

	if !g.messages[0].revealed {
		t.Fatal("the first tap did not reveal the message")
	}

	g.updateChatRow(0, row)

	if got := label.Text(); got != "hunter2" {
		t.Errorf("the revealed row draws %q, want the text", got)
	}

	g.selectChatRow(0)

	if got := g.app.Clipboard().Content(); got != "You: hunter2" {
		t.Errorf("the second tap copied %q, want the message", got)
	}
}

// A hidden send carries the flag in the header, and the local row is masked
// too (#469).
func TestAHiddenSendCarriesTheFlag(t *testing.T) {
	g := newScreenGUI(t, false)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.toggleHidden()
	g.send("hunter2")

	payload := sess.wait(t)

	frame, err := wire.Parse(payload)
	if err != nil {
		t.Fatalf("the sent payload does not parse: %v", err)
	}

	if !frame.Header.Hidden {
		t.Error("the sent frame does not carry the hidden flag")
	}

	if string(frame.Payload) != "hunter2" {
		t.Errorf("the sent payload is %q, want the message", frame.Payload)
	}

	if len(g.messages) != 1 || !g.messages[0].hidden {
		t.Fatalf("the local transcript is %+v, want one hidden row", g.messages)
	}

	if got := g.messages[0].body(); got != hiddenBody {
		t.Errorf("the sent row draws %q, want the mask", got)
	}
}

// A hidden message that arrives already masked needs no local choice: the
// sender's flag travels with it (#469).
func TestAHiddenFrameArrivesMasked(t *testing.T) {
	g := newScreenGUI(t, false)

	payload, err := wire.Encode(wire.Header{Kind: wire.KindText, Hidden: true}, []byte("hunter2"))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	g.receiveFrame("laptop", payload)

	if len(g.messages) != 1 || !g.messages[0].hidden {
		t.Fatalf("the transcript holds %+v, want one hidden row", g.messages)
	}

	if got := g.messages[0].body(); got != hiddenBody {
		t.Errorf("the arriving row draws %q, want the mask", got)
	}

	if got := g.messages[0].line(); !strings.Contains(got, "hunter2") {
		t.Errorf("line() = %q, want the secret a deliberate copy keeps", got)
	}
}
