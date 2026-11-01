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

// A tap has to keep doing what it did before the text was wrapped, on every
// platform: the driver dispatches it to this widget whichever screen it is, so
// a flag that ignored it on a desktop silently took the click away (#236).
func TestTextForwardsATapOnEveryPlatform(t *testing.T) {
	for _, touch := range []bool{true, false} {
		text := NewText("hello", touch)

		taps := 0
		text.SetOnTapped(func() { taps++ })

		text.Tapped(&fyne.PointEvent{})

		if taps != 1 {
			t.Errorf("touch=%v: the tap ran %d times, want 1", touch, taps)
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

// The gesture layer carries exactly the two gestures Text takes, and neither of
// the two it must not take: a Draggable layer would take the desktop drag from
// the selection underneath, and a Touchable one would stop the pane scrolling on
// a phone.
func TestTapCatcherTakesOnlyTheTapAndTheLongPress(t *testing.T) {
	var catcher any = newTapCatcher(nil, nil)

	if _, ok := catcher.(fyne.Tappable); !ok {
		t.Error("the gesture layer takes no tap, so a row would stay dead")
	}

	if _, ok := catcher.(fyne.SecondaryTappable); !ok {
		t.Error("the gesture layer takes no long press, so nothing could be selected on a phone")
	}

	if _, ok := catcher.(fyne.Draggable); ok {
		t.Error("the gesture layer is Draggable, so a desktop drag would stop selecting the address")
	}

	if _, ok := catcher.(mobile.Touchable); ok {
		t.Error("the gesture layer is mobile.Touchable, so the driver would stop at it and the pane would not scroll")
	}
}

// A line with nothing to do keeps Fyne's own desktop gesture: no layer, so the
// driver still finds the label's selection child and its menu.
func TestTextWithoutAnActionLeavesTheDesktopGestureAlone(t *testing.T) {
	text := NewText("hello", false)

	if text.catcher.Visible() {
		t.Error("a text with no action put a gesture layer up, where Fyne's own selection should stay in charge")
	}
}

// Giving the text an action is what brings its layer up, and the layer is what
// makes the tap reachable above a selectable label (#280).
func TestTextWithAnActionPutsItsGestureLayerUp(t *testing.T) {
	text := NewText("hello", false)

	taps := 0
	text.SetOnTapped(func() { taps++ })

	if !text.catcher.Visible() {
		t.Fatal("giving the text an action left its gesture layer hidden")
	}

	// The tap the user makes lands on the layer; it has to run the row's action.
	text.catcher.Tapped(&fyne.PointEvent{})

	if taps != 1 {
		t.Errorf("the gesture layer ran the action %d times, want 1", taps)
	}
}

// A tappable row owns the press once its layer is up, so the wrapper offers the
// menu Fyne's selection child used to: Copy over the selected text, or over the
// whole line when nothing is selected.
func TestTappableTextOffersCopyOnADesktop(t *testing.T) {
	app := test.NewApp()
	t.Cleanup(app.Quit)

	text := NewText("hello there", false)
	text.SetOnTapped(func() {})

	win := test.NewWindow(container.NewVBox(container.NewPadded(text)))
	t.Cleanup(win.Close)
	win.Resize(fyne.NewSize(300, 100))

	text.TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(20, 10), Position: fyne.NewPos(20, 10)})

	top := win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("a right click on a tappable row offered no Copy menu")
	}

	app.Clipboard().SetContent("")
	tapFirstMenuItem(t, top)

	if got := app.Clipboard().Content(); got != "hello there" {
		t.Errorf("Copy copied %q, want the whole line", got)
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
