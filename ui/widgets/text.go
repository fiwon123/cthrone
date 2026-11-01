package widgets

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// selectionWindow is how long a long press leaves a label selectable.
//
// It is not decoration. A selectable label is a Draggable, and the mobile
// driver hands a drag to the deepest Draggable — so while a label is
// selectable, a finger dragged across it selects text instead of scrolling the
// pane. That is the trade-off the package comment on Label describes, and it is
// why a touch screen gets selection on request rather than always. The window
// bounds the cost: the handles are there to be looked at and dragged, and a
// few seconds later the pane scrolls by touch again.
// It is a variable so a test can shorten it rather than wait five seconds.
var selectionWindow = 5 * time.Second

// Text is a label whose text a touch screen can select and copy with a long
// press — without taking the pane's scroll gesture while nobody is selecting.
//
// The gesture rules here are Fyne's driver's, not a preference:
//
//   - the mobile driver calls TappedSecondary on the deepest object under a
//     press held for 300 ms, which is the long press;
//   - it picks the tap target from any of Tappable, SecondaryTappable,
//     Touchable or DoubleTappable, so a Text that handled only the long press
//     would swallow plain taps as well — hence the tap callback, which keeps
//     the transcript's tap-to-copy working;
//   - it picks the drag target from Draggable or Touchable, and does not scroll
//     when that object is not Draggable. So Text must implement neither: a
//     widget that did would trade scrolling for a gesture nobody asked for.
//
// On a desktop none of that applies: the label is selectable from the start,
// drag-selection and the right-click Copy menu are Fyne's own, and the wrapper
// never sees the gestures because Fyne's selection child is deeper than it.
type Text struct {
	widget.BaseWidget

	label    *widget.Label
	touch    bool
	onTapped func()

	// restore turns selection off again after selectionWindow. It is kept so a
	// second long press extends the window instead of stacking timers.
	restore *time.Timer
}

// NewText is a label with long-press selection on a touch screen. touch is
// whether this window is on a touch device.
func NewText(text string, touch bool) *Text {
	return newText(text, touch, false)
}

// NewHeading is NewText in bold: a section title, which is still text a user
// may need to copy.
func NewHeading(text string, touch bool) *Text {
	return newText(text, touch, true)
}

func newText(text string, touch, bold bool) *Text {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	label.TextStyle = fyne.TextStyle{Bold: bold}

	// A desktop selects by dragging, which is what a mouse is for. On a touch
	// screen that same drag scrolls, so selection waits for a long press.
	label.Selectable = !touch

	text2 := &Text{label: label, touch: touch}
	text2.ExtendBaseWidget(text2)

	return text2
}

// CreateRenderer draws the label. The renderer's object is the label itself and
// not a container this struct also holds: a wrapper that returned something it
// embedded had its position counted twice by the driver's walk (#186, #189).
func (t *Text) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.label)
}

// SetText replaces the text.
func (t *Text) SetText(text string) {
	t.label.SetText(text)
}

// Text is the text it is showing.
func (t *Text) Text() string {
	return t.label.Text
}

// TextStyle is the style the text is drawn in.
func (t *Text) TextStyle() fyne.TextStyle {
	return t.label.TextStyle
}

// SetTextStyle sets the style, so a caller can make this line bold.
func (t *Text) SetTextStyle(style fyne.TextStyle) {
	t.label.TextStyle = style
	t.label.Refresh()
}

// Selectable reports whether the text can be selected at this moment: always on
// a desktop, only inside the window after a long press on a touch screen.
func (t *Text) Selectable() bool {
	return t.label.Selectable
}

// SetOnTapped sets what a plain tap does, which is what the tap did before the
// text was wrapped: tapping a transcript row copies it, and tapping a status
// line does nothing.
func (t *Text) SetOnTapped(tapped func()) {
	t.onTapped = tapped
}

// Tapped is a plain tap: on every platform the driver dispatches it here,
// because Text is the deepest object that takes one — the label's selection
// child does not receive taps, whatever the platform. So the tap has to keep
// doing what it did before the text was wrapped: filling an address, copying a
// row.
//
// The guard on the touch-screen flag that this used to carry was wrong, and it
// cost the Devices tab its clicks on a desktop (#236): the belief was that the
// selection child handled desktop taps, and nothing did.
func (t *Text) Tapped(*fyne.PointEvent) {
	if t.onTapped == nil {
		return
	}

	t.onTapped()
}

// TappedSecondary is the long press on a touch screen: it starts Fyne's own
// selection at the point that was pressed and offers Copy.
func (t *Text) TappedSecondary(ev *fyne.PointEvent) {
	if !t.touch {
		// A desktop has Fyne's selection from the start; its selection child is
		// deeper than this widget and receives the gesture instead.
		return
	}

	t.selectAt(ev)
	t.showCopyMenu(ev)
}

// selectAt starts a selection at the pressed point: Fyne's selection machinery
// with its handles, and the word under the finger selected, exactly as a
// double-tap selects it on a desktop.
func (t *Text) selectAt(ev *fyne.PointEvent) {
	t.label.Selectable = true
	t.label.Refresh()

	// The selection lives in a child widget the label's renderer owns. There is
	// no exporting API to say "select the word at this point", but the child is
	// reachable through the renderer and it does implement Fyne's own gesture.
	if double, ok := t.selectionChild().(fyne.DoubleTappable); ok {
		double.DoubleTapped(ev)
	}

	t.armSelectionWindow()
}

// selectionChild is the label's selection widget, once it has one.
//
// The renderer is built here rather than fetched from the canvas cache because
// a driver exposes no such lookup, and because it does not need to be the same
// renderer: the selection widget is a field of the label itself, so this one
// and the one the canvas draws are the same object.
func (t *Text) selectionChild() fyne.CanvasObject {
	renderer := t.label.CreateRenderer()
	if renderer == nil {
		return nil
	}

	objects := renderer.Objects()
	if len(objects) == 0 {
		return nil
	}

	// Objects() is [selection, text] while the label is selectable.
	return objects[0]
}

// armSelectionWindow ends selection after selectionWindow, so the pane scrolls
// by touch again.
func (t *Text) armSelectionWindow() {
	if t.restore != nil {
		t.restore.Stop()
	}

	t.restore = time.AfterFunc(selectionWindow, func() {
		// A window can be closed while the window is still ticking. There is
		// nothing left to restore then, and no application to restore it on.
		if fyne.CurrentApp() == nil {
			return
		}

		fyne.Do(func() {
			t.label.Selectable = false
			t.label.Refresh()
		})
	})
}

// showCopyMenu offers the copy a phone has no keyboard shortcut for: the
// selection, or the whole line when the selection came up empty.
func (t *Text) showCopyMenu(ev *fyne.PointEvent) {
	app := fyne.CurrentApp()
	if app == nil || app.Driver() == nil {
		return
	}

	canvas := app.Driver().CanvasForObject(t)
	if canvas == nil {
		return
	}

	// Read at tap time, not when the menu is built: the user may have dragged
	// the handles wider before choosing Copy.
	copied := func() string {
		if selection := t.label.SelectedText(); selection != "" {
			return selection
		}

		return t.label.Text
	}

	menu := fyne.NewMenu("", fyne.NewMenuItem("Copy", func() {
		app.Clipboard().SetContent(copied())
	}))

	widget.ShowPopUpMenuAtPosition(menu, canvas, ev.AbsolutePosition)
}
