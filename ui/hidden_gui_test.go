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

	eye, _ := rowButtons(t, row)

	return eye
}

// rowButtons finds the two controls a row carries, in the order the sender line
// puts them: the eye, then the copy button (#503).
func rowButtons(t *testing.T, row fyne.CanvasObject) (*widgets.CopyButton, *widgets.CopyButton) {
	t.Helper()

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want a container", row)
	}

	caption, ok := box.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("row[0] is %T, want the caption cell", box.Objects[0])
	}

	var buttons []*widgets.CopyButton

	for _, object := range caption.Objects {
		part, ok := object.(*fyne.Container)
		if !ok {
			continue
		}

		for _, child := range part.Objects {
			if inner, ok := child.(*fyne.Container); ok {
				for _, item := range inner.Objects {
					if button, ok := item.(*widgets.CopyButton); ok {
						buttons = append(buttons, button)
					}
				}
			}
		}
	}

	if len(buttons) < 2 {
		t.Fatalf("the row carries %d control(s), want the eye and the copy button", len(buttons))
	}

	return buttons[0], buttons[1]
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

	if got := label.Text(); got != "hunter2" {
		t.Errorf("the revealed row draws %q, want the text", got)
	}

	// The control toggles both ways: a message revealed by mistake can be hidden
	// again, and the icon is the control's state (#503).
	if !eye.Visible() {
		t.Fatal("a revealed row dropped its mask control, so it cannot be hidden again")
	}

	if got, want := eye.Icon(), theme.VisibilityOffIcon(); got == nil || got.Name() != want.Name() {
		t.Errorf("a revealed row's eye shows %v, want the hide icon", got)
	}

	test.Tap(eye)
	g.updateChatRow(0, row)

	if !g.messages[0].masked() {
		t.Error("the second press did not hide the message again")
	}

	if got := label.Text(); got != hiddenBody {
		t.Errorf("the re-hidden row draws %q, want the mask", got)
	}

	// The copy is the row's button now, and it takes the text, not "You: ...".
	g.app.Clipboard().SetContent("")
	test.Tap(rowCopyButton(t, row))

	if got := g.app.Clipboard().Content(); got != "hunter2" {
		t.Errorf("the row's copy button copied %q, want the message text", got)
	}
}

// A normal message can be hidden from its own row too; the state is local.
// A message the reader can already see has no mask control on it: the eye is for
// a row that is masked, not for one that could be (#496).
func TestAVisibleRowHasNoEye(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "hello"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	if eye := rowReveal(t, row); eye.Visible() {
		t.Error("a visible message offers a mask control")
	}

	// Sending with the composer's eye still masks it for the peer and here; it
	// is the only way to hide a message now, and the row then offers the reveal.
	g.mu.Lock()
	g.messages[0].hidden = true
	g.mu.Unlock()

	g.updateChatRow(0, row)

	label, _, _, _, _ := chatRowObjects(t, row)
	if got := label.Text(); got != hiddenBody {
		t.Errorf("the hidden row draws %q, want the mask", got)
	}

	eye := rowReveal(t, row)
	if !eye.Visible() {
		t.Fatal("a masked row offers no reveal control")
	}

	test.Tap(eye)
	g.updateChatRow(0, row)

	if got := label.Text(); got != "hello" {
		t.Errorf("the revealed row draws %q, want the text", got)
	}

	// A hidden message keeps its control in both states; a message that was
	// never hidden never had one.
	if !eye.Visible() {
		t.Error("a revealed row lost the control that hides it again")
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
