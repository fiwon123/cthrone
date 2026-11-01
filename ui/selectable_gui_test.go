//go:build gui

package ui

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// These tests pin the rule #179 fixed — a drag on text must reach the scroll
// container on a phone — and the exception #213 added to it: the text a user
// reads is selected by a long press, while the drag stays the pane's.

func newScreenGUI(t *testing.T, mobile bool) *gui {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	app := test.NewApp()
	t.Cleanup(app.Quit)

	win := app.NewWindow("cthrone")
	// A desktop window is wide enough for two columns; the test driver's
	// default canvas is 215px, which the same rule would stack.
	if !mobile {
		win.Resize(fyne.NewSize(900, 640))
	}
	g := &gui{app: app, win: win, mobile: mobile}
	g.build()
	t.Cleanup(g.stopCopyHint)

	return g
}

func TestTouchLabelsDoNotTakeTheDrag(t *testing.T) {
	g := newScreenGUI(t, true)

	if g.status.Selectable() {
		t.Error("the status line is selectable on touch")
	}
	if g.hint.Selectable() {
		t.Error("the setup hint is selectable on touch")
	}
	if g.credential.Selectable() {
		t.Error("the credential line is selectable on touch")
	}
	if g.aboutCredential.Selectable() {
		t.Error("the About credential line is selectable on touch")
	}
	if g.aboutNote.Selectable() {
		t.Error("the update note is selectable on touch")
	}
}

// A phone's text is selectable by a long press, not by the drag that scrolls
// the pane: before the press it is inert, after it Fyne's own selection is on.
func TestTouchTextSelectsOnALongPress(t *testing.T) {
	g := newScreenGUI(t, true)

	g.status.SetText("Connection failed: the host refused the handshake")
	g.win.Resize(fyne.NewSize(320, 480))
	g.win.Content().Refresh()

	g.status.TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(20, 10)})

	if !g.status.Selectable() {
		t.Fatal("a long press left the status line unselectable, so nothing could be copied from it")
	}

	if g.win.Canvas().Overlays().Top() == nil {
		t.Error("a long press on the status line offered no Copy menu")
	}
}

// The transcript's tap-to-copy is the gesture a phone already had, and wrapping
// the text in a long-press widget must not take it away: tap still copies the
// row, and a different tap copies a different row.
func TestTouchRowStillCopiesOnATap(t *testing.T) {
	g := newScreenGUI(t, true)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "bob", text: "the first"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	text, _, _, _ := chatRowObjects(t, row)

	g.app.Clipboard().SetContent("")
	text.Tapped(&fyne.PointEvent{})

	if got := g.app.Clipboard().Content(); !strings.Contains(got, "the first") {
		t.Errorf("a tap copied %q, want the message", got)
	}
}

func TestDesktopLabelsStaySelectable(t *testing.T) {
	g := newScreenGUI(t, false)

	if !g.status.Selectable() {
		t.Error("the status line is not selectable on a desktop")
	}
	if !g.hint.Selectable() {
		t.Error("the setup hint is not selectable on a desktop")
	}
}

func TestTouchCheckRowsAreInert(t *testing.T) {
	g := newScreenGUI(t, true)

	for _, row := range []*widgets.CheckRow{g.tlsCheck, g.insecureCheck} {
		if caption := checkRowCaption(t, row); caption.Selectable {
			t.Errorf("check caption %q is selectable on touch", caption.Text)
		}
	}
}

func TestDesktopCheckRowsAreSelectable(t *testing.T) {
	g := newScreenGUI(t, false)

	if caption := checkRowCaption(t, g.tlsCheck); !caption.Selectable {
		t.Error("check caption is not selectable on a desktop")
	}
}

func TestTouchSetupFormStacksInertCaptions(t *testing.T) {
	g := newScreenGUI(t, true)

	form, ok := g.formHolder.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("setup form is %T, want a container", g.formHolder.Objects[0])
	}
	if reflect.TypeOf(form.Layout) != reflect.TypeOf(layout.NewVBoxLayout()) {
		t.Fatalf("setup form layout is %T, want stacked lines", form.Layout)
	}

	for _, line := range form.Objects {
		box, ok := line.(*fyne.Container)
		if !ok {
			t.Fatalf("form line is %T, want a container", line)
		}

		caption, ok := box.Objects[0].(*widget.Label)
		if !ok {
			t.Fatalf("form line starts with %T, want its caption", box.Objects[0])
		}
		if caption.Selectable {
			t.Errorf("setup caption %q is selectable on touch", caption.Text)
		}
		if caption.TextStyle.Bold {
			t.Errorf("setup caption %q is bold; it reads as a heading, not a form label", caption.Text)
		}
	}
}

func TestDesktopSetupFormUsesTwoColumns(t *testing.T) {
	g := newScreenGUI(t, false)

	form, ok := g.formHolder.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("setup form is %T, want a container", g.formHolder.Objects[0])
	}
	if reflect.TypeOf(form.Layout) != reflect.TypeOf(layout.NewFormLayout()) {
		t.Fatalf("setup form layout is %T, want the familiar two columns", form.Layout)
	}

	caption, ok := form.Objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("first cell is %T, want the caption", form.Objects[0])
	}
	if !caption.Selectable {
		t.Error("setup caption is not selectable on a desktop")
	}
	if caption.TextStyle.Bold {
		t.Error("setup caption is bold; it reads as a heading, not a form label")
	}
}

func TestAboutHeaderStacksNameAboveVersion(t *testing.T) {
	g := newScreenGUI(t, false)

	box, ok := g.aboutHeader("v0.8.3").(*fyne.Container)
	if !ok {
		t.Fatalf("header is %T, want a container", g.aboutHeader("v0.8.3"))
	}
	if len(box.Objects) != 2 {
		t.Fatalf("header holds %d objects, want the name and the version", len(box.Objects))
	}

	name, ok := box.Objects[0].(*widgets.Text)
	if !ok {
		t.Fatalf("header[0] is %T, want the name", box.Objects[0])
	}
	if name.Text() != "cthrone" || !name.TextStyle().Bold {
		t.Errorf("name = %q (bold %v), want the bold app name", name.Text(), name.TextStyle().Bold)
	}

	version, ok := box.Objects[1].(*widgets.Text)
	if !ok {
		t.Fatalf("header[1] is %T, want the version", box.Objects[1])
	}
	if version.Text() != "v0.8.3" {
		t.Errorf("version = %q, want %q", version.Text(), "v0.8.3")
	}
}

// checkRowCaption returns the label a check row carries as its caption.
func checkRowCaption(t *testing.T, row *widgets.CheckRow) *widget.Label {
	t.Helper()

	content, ok := test.WidgetRenderer(row).Objects()[0].(*fyne.Container)
	if !ok {
		t.Fatalf("check row content is %T, want the container it draws", test.WidgetRenderer(row).Objects()[0])
	}

	for _, object := range content.Objects {
		if caption, ok := object.(*widget.Label); ok {
			return caption
		}
	}

	t.Fatal("check row holds no caption label")

	return nil
}
