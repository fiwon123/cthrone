//go:build gui

package ui

import (
	"reflect"
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
// A tap on a phone copies nothing either: the long press is the copy gesture,
// and a tap is where a selection starts (#496).
func TestATouchTapOnARowCopiesNothing(t *testing.T) {
	g := newScreenGUI(t, true)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "bob", text: "the first"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	text, _, _, _, _ := chatRowObjects(t, row)

	g.app.Clipboard().SetContent("unchanged")
	text.Tapped(&fyne.PointEvent{})

	if got := g.app.Clipboard().Content(); got != "unchanged" {
		t.Errorf("a tap copied %q, want nothing", got)
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
		// A row with no caption is a control that speaks for itself — the
		// security switches (#291) — and is its own line, not a caption over a
		// field.
		if _, ok := line.(*widgets.CheckRow); ok {
			continue
		}

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

// headerParts finds the app icon, the name and the version wherever the header
// puts them: side by side on a desktop, the version under the name on a phone
// (#329).
func headerParts(t *testing.T, root fyne.CanvasObject) (*widget.Icon, *widgets.Text) {
	t.Helper()

	var icon *widget.Icon
	var texts []*widgets.Text

	var walk func(obj fyne.CanvasObject, depth int)
	walk = func(obj fyne.CanvasObject, depth int) {
		if obj == nil || depth > 8 {
			return
		}

		switch part := obj.(type) {
		case *widget.Icon:
			icon = part
		case *widgets.Text:
			texts = append(texts, part)
		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child, depth+1)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(part).Objects() {
				walk(child, depth+1)
			}
		}
	}

	walk(root, 0)

	if icon == nil || len(texts) < 1 {
		t.Fatalf("the header holds icon=%v and %d text(s), want the logo and the name with its version", icon != nil, len(texts))
	}

	return icon, texts[0]
}

// The header carries the app's mark, its name, and the version immediately after
// it — one group, not a version pinned to the screen's edge (#329, #343).
func TestAboutHeaderShowsTheLogoTheNameAndTheVersion(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			header := g.aboutHeader("v0.8.3")

			box, ok := header.(*fyne.Container)
			if !ok || len(box.Objects) != 2 {
				t.Fatalf("the header is %T holding %d object(s), want the mark and one line of name and version", header, len(box.Objects))
			}

			icon, title := headerParts(t, header)

			if !icon.Visible() {
				t.Error("the header draws no logo")
			}

			if icon.Resource == nil {
				t.Error("the logo has no resource, so it paints nothing")
			}

			if title.Text() != "cthrone  v0.8.3" || !title.TextStyle().Bold {
				t.Errorf("the heading reads %q (bold %v), want the name and the version on one line", title.Text(), title.TextStyle().Bold)
			}
		})
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

// The Setup tab asks in the order a user works: the device and the address, the
// carrier, then the security switches that belong to it, then the options
// (#291). The check rows used to sit above the whole form, so the first thing on
// the tab was a decision the user rarely changes.
func TestSetupAsksForTheDeviceBeforeTheSwitches(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)
			g.moreOptions = true
			g.rebuildSetupForm()
			g.win.Resize(fyne.NewSize(420, 900))
			g.win.Content().Refresh()

			order := fieldOrder(t, g.formHolder.Objects[0])

			want := []fyne.CanvasObject{
				fyne.CanvasObject(g.nameEntry),
				fyne.CanvasObject(g.hostEntry),
				fyne.CanvasObject(g.transport),
				fyne.CanvasObject(g.tlsCheck),
				fyne.CanvasObject(g.insecureCheck),
				fyne.CanvasObject(g.moreBtn),
			}

			last := -1
			for i, field := range want {
				at, ok := order[field]
				if !ok {
					t.Fatalf("field %d (%T) is not in the form", i, field)
				}

				if at <= last {
					t.Fatalf("field %d (%T) is at line %d, after the one before it at %d", i, field, at, last)
				}

				last = at
			}
		})
	}
}

// fieldOrder maps each field the form holds to the line it is on: a stacked
// form wraps a captioned row in a container, and a control with no caption is
// the line itself.
func fieldOrder(t *testing.T, form fyne.CanvasObject) map[fyne.CanvasObject]int {
	t.Helper()

	box, ok := form.(*fyne.Container)
	if !ok {
		t.Fatalf("the form is %T, want a container", form)
	}

	order := make(map[fyne.CanvasObject]int)

	// Two columns: caption and field alternate through one object list.
	if reflect.TypeOf(box.Layout) == reflect.TypeOf(layout.NewFormLayout()) {
		for i := 1; i < len(box.Objects); i += 2 {
			order[box.Objects[i]] = i / 2
		}

		return order
	}

	for i, line := range box.Objects {
		if wrapped, ok := line.(*fyne.Container); ok && len(wrapped.Objects) > 1 {
			order[wrapped.Objects[1]] = i

			continue
		}

		order[line] = i
	}

	return order
}
