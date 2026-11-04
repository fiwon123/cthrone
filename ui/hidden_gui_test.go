//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/fiwon123/cthrone/internal/wire"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// rowReveal finds the eye a row carries. It is the only CopyButton in the
// caption cell; Save and Open live in the row's own box (#474).
func rowReveal(t *testing.T, row fyne.CanvasObject) *widgets.CopyButton {
	t.Helper()

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want a container", row)
	}

	caption, ok := box.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("row[0] is %T, want the caption cell", box.Objects[0])
	}

	for _, object := range caption.Objects {
		if button, ok := object.(*widgets.CopyButton); ok {
			return button
		}
	}

	t.Fatal("the row carries no reveal control")

	return nil
}

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

// A hidden row draws the mask, and its own eye shows it again. A tap on the row
// copies the text even while masked: the eye is the direct control, the tap is
// the copy (#474).
func TestAHiddenRowHasItsOwnEye(t *testing.T) {
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

	eye := rowReveal(t, row)
	if !eye.Visible() {
		t.Fatal("a text row offers no reveal control")
	}

	// The icon is the control's state, and it is only set when it changes: a
	// row repainted in the same theme must not re-theme (and re-parse) an SVG
	// (#491).
	if got, want := eye.Icon(), theme.VisibilityIcon(); got == nil || got.Name() != want.Name() {
		t.Fatalf("a masked row's eye shows %v, want the show icon", got)
	}

	test.Tap(eye)
	g.updateChatRow(0, row)

	if !g.messages[0].revealed {
		t.Error("the eye did not reveal the message")
	}

	if got, want := eye.Icon(), theme.VisibilityOffIcon(); got == nil || got.Name() != want.Name() {
		t.Errorf("a revealed row's eye shows %v, want the hide icon", got)
	}

	if got := label.Text(); got != "hunter2" {
		t.Errorf("the revealed row draws %q, want the text", got)
	}

	// The same control hides it again.
	test.Tap(eye)
	g.updateChatRow(0, row)

	if !g.messages[0].masked() {
		t.Error("the second press did not hide the message")
	}

	if got := label.Text(); got != hiddenBody {
		t.Errorf("the hidden row draws %q, want the mask", got)
	}

	// A tap on the row is the copy, and it takes the text, not "You: ...".
	g.app.Clipboard().SetContent("")
	g.selectChatRow(0)

	if got := g.app.Clipboard().Content(); got != "hunter2" {
		t.Errorf("a tap copied %q, want the message text", got)
	}
}

// A normal message can be hidden from its own row too; the state is local.
func TestAnyTextRowCanBeHidden(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "hello"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	label, _, _, _, _ := chatRowObjects(t, row)
	eye := rowReveal(t, row)

	test.Tap(eye)
	g.updateChatRow(0, row)

	if !g.messages[0].hidden || !g.messages[0].masked() {
		t.Error("the eye did not hide a normal message")
	}

	if got := label.Text(); got != hiddenBody {
		t.Errorf("the hidden row draws %q, want the mask", got)
	}

	test.Tap(eye)
	g.updateChatRow(0, row)

	if got := label.Text(); got != "hello" {
		t.Errorf("the revealed row draws %q, want the text", got)
	}
}

// A file row has no eye: a file is not a message to mask (#474).
func TestAFileRowHasNoReveal(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", file: &fileNote{name: "doc.pdf", state: transferFailed}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	if eye := rowReveal(t, row); eye.Visible() {
		t.Error("a file row offered a hide/show eye")
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

	if got := g.messages[0].copyText(); got != "hunter2" {
		t.Errorf("copyText() = %q, want the secret a deliberate copy keeps", got)
	}
}
