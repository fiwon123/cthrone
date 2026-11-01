package widgets

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// The mobile driver picks the drag target from Draggable or Touchable, and it
// does not scroll when that object is not Draggable. So a Text that took either
// interface would trade the pane's scroll for a gesture nobody asked for — the
// reason these two assertions are negative ones.
func TestTextTakesTheTapAndTheLongPressOnly(t *testing.T) {
	var text any = NewText("hello", true)

	if _, ok := text.(fyne.Tappable); !ok {
		t.Error("Text does not take a tap, so the row's tap-to-copy would be lost")
	}

	if _, ok := text.(fyne.SecondaryTappable); !ok {
		t.Error("Text does not take a long press, so nothing could be selected on a phone")
	}

	if _, ok := text.(fyne.Draggable); ok {
		t.Error("Text is Draggable, so dragging it would not scroll the pane")
	}

	if _, ok := text.(mobile.Touchable); ok {
		t.Error("Text is mobile.Touchable, so the driver would stop at it and the pane would not scroll")
	}
}

// A desktop selects by dragging; a touch screen must not, or the pane cannot be
// scrolled by touch at all.
func TestTextSelectsByDefaultOnlyOnADesktop(t *testing.T) {
	if !NewText("hello", false).Selectable() {
		t.Error("a desktop label is not selectable")
	}

	if NewText("hello", true).Selectable() {
		t.Error("a touch label is selectable before any long press")
	}
}

// The text is a field the window keeps for its lifetime, so replacing it has to
// reach the label underneath.
func TestTextKeepsItsString(t *testing.T) {
	text := NewText("first", true)

	if got := text.Text(); got != "first" {
		t.Fatalf("Text() = %q, want %q", got, "first")
	}

	text.SetText("second")

	if got := text.Text(); got != "second" {
		t.Errorf("Text() = %q after SetText, want %q", got, "second")
	}
}

// A tap has to keep doing what it did before the text was wrapped: the
// transcript's row is selected, a status line does nothing.
func TestTextForwardsATapOnlyOnATouchScreen(t *testing.T) {
	for _, touch := range []bool{true, false} {
		text := NewText("hello", touch)

		taps := 0
		text.SetOnTapped(func() { taps++ })

		text.Tapped(&fyne.PointEvent{})

		want := 0
		if touch {
			want = 1
		}

		if taps != want {
			t.Errorf("touch=%v: the tap ran %d times, want %d", touch, taps, want)
		}
	}
}

// A long press on a touch screen starts a selection and offers Copy; the
// selection is Fyne's own, so the label has to be selectable for it to exist.
func TestTextLongPressSelectsAndOffersCopy(t *testing.T) {
	app := test.NewApp()
	t.Cleanup(app.Quit)

	shortenSelectionWindow(t)

	text := NewText("morning bob the network is down", true)
	win := test.NewWindow(container.NewVBox(container.NewPadded(text)))
	t.Cleanup(win.Close)
	win.Resize(fyne.NewSize(300, 200))

	text.TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(40, 12), Position: fyne.NewPos(40, 12)})

	if !text.Selectable() {
		t.Error("the long press did not make the text selectable, so there is nothing to select")
	}

	if got := text.label.SelectedText(); got == "" {
		t.Error("the long press selected nothing")
	}

	top := win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("the long press offered no Copy menu")
	}

	app.Clipboard().SetContent("")
	tapFirstMenuItem(t, top)

	if got := app.Clipboard().Content(); got == "" {
		t.Error("the Copy entry copied nothing")
	}
}

// The selection does not last: while a label is selectable a drag selects
// instead of scrolling, which is the trade-off this whole widget exists to
// bound. When the window closes, the pane scrolls by touch again.
func TestTextSelectionEndsOnItsOwn(t *testing.T) {
	app := test.NewApp()
	t.Cleanup(app.Quit)

	shortenSelectionWindow(t)

	text := NewText("morning bob", true)
	win := test.NewWindow(container.NewVBox(text))
	t.Cleanup(win.Close)
	win.Resize(fyne.NewSize(300, 100))

	text.TappedSecondary(&fyne.PointEvent{})

	if !text.Selectable() {
		t.Fatal("the long press did not make the text selectable")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !text.Selectable() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the text stayed selectable, so the pane would never scroll by touch again")
}

// A desktop's long press is Fyne's own: the wrapper must not start a selection
// of its own or pop a menu over it.
func TestTextLeavesTheDesktopGestureAlone(t *testing.T) {
	app := test.NewApp()
	t.Cleanup(app.Quit)

	text := NewText("hello", false)
	win := test.NewWindow(container.NewVBox(text))
	t.Cleanup(win.Close)
	win.Resize(fyne.NewSize(300, 100))

	text.TappedSecondary(&fyne.PointEvent{})

	if win.Canvas().Overlays().Top() != nil {
		t.Error("the wrapper popped its own menu on a desktop, where Fyne already offers one")
	}
}

// The widget draws: a wrapper the paint walk treats as a leaf would show a flat
// background, which is what shipped as a blank window in v0.8.3.
func TestTextPaintsItsString(t *testing.T) {
	newPaintApp(t)

	if got := paintedColours(t, NewText("hello there", false), fyne.NewSize(200, 40)); got < 2 {
		t.Errorf("the text painted %d colours, want the string and the background", got)
	}
}

// tapFirstMenuItem taps the menu's only entry — Copy. A Copy wired to nothing
// is the failure that looks like success until the clipboard is pasted.
func tapFirstMenuItem(t *testing.T, overlay fyne.CanvasObject) {
	t.Helper()

	popup := findPopup(overlay, 0)
	if popup == nil {
		t.Fatalf("the overlay holds no menu: %T", overlay)
	}

	if len(popup.Items) == 0 {
		t.Fatal("the menu has no entries")
	}

	item, ok := popup.Items[0].(fyne.Tappable)
	if !ok {
		t.Fatalf("the menu entry is %T, want something tappable", popup.Items[0])
	}

	test.Tap(item)
}

func findPopup(object fyne.CanvasObject, depth int) *widget.PopUpMenu {
	if object == nil || depth > 10 {
		return nil
	}

	if popup, ok := object.(*widget.PopUpMenu); ok {
		return popup
	}

	if box, ok := object.(*fyne.Container); ok {
		for _, child := range box.Objects {
			if popup := findPopup(child, depth+1); popup != nil {
				return popup
			}
		}
	}

	if drawn, ok := object.(fyne.Widget); ok {
		for _, child := range test.WidgetRenderer(drawn).Objects() {
			if popup := findPopup(child, depth+1); popup != nil {
				return popup
			}
		}
	}

	return nil
}

// shortenSelectionWindow makes the restore prompt for a test, and puts it back.
func shortenSelectionWindow(t *testing.T) {
	t.Helper()

	previous := selectionWindow
	selectionWindow = 20 * time.Millisecond
	t.Cleanup(func() { selectionWindow = previous })
}

// The label underneath is what a window keeps a handle to; a test that reads it
// through SelectedText needs the widget to be a *widget.Label and not a copy.
var _ fyne.Widget = (*widget.Label)(nil)
