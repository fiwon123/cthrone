package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// CopyButton is a button whose label can be copied.
//
// A Fyne button paints its own text and offers no selectable-text API, so the
// label cannot be drag-selected without replacing the widget — and a selection
// overlay would swallow the tap the button exists for. The gesture is therefore
// a long press (a right-click on a desktop, which is the same event): the label
// goes to the clipboard and a short hint says so, while a normal press still
// runs the button.
//
// The widget is not a container. It holds one, and the toolkit draws that
// container: a wrapper struct that both embedded the container and returned it
// from CreateRenderer would have the container's position counted twice by the
// driver's walk — once as the widget, once as the renderer object — and every
// child would paint at its hit position plus the widget's own position. The
// container therefore stays private and sits at the widget's origin; the button
// and the catcher inside it are the objects that draw.
type CopyButton struct {
	widget.BaseWidget

	button   *widget.Button
	catcher  *copyCatcher
	content  *fyne.Container
	copyText string
	onCopied func(string)
}

var (
	_ fyne.Tappable          = (*CopyButton)(nil)
	_ fyne.SecondaryTappable = (*CopyButton)(nil)
)

// NewButton builds a CopyButton with the given label and tap handler.
//
// onCopied runs after the label has reached the clipboard, so the caller can
// confirm the gesture; the widget owns the copy itself, because every caller
// would otherwise repeat the nil-app and empty-label checks.
func NewButton(text string, tapped func(), onCopied func(string)) *CopyButton {
	return newCopyButton(widget.NewButton(text, tapped), text, onCopied)
}

// NewButtonWithIcon is NewButton with an icon beside the label.
func NewButtonWithIcon(text string, icon fyne.Resource, tapped func(), onCopied func(string)) *CopyButton {
	return newCopyButton(widget.NewButtonWithIcon(text, icon, tapped), text, onCopied)
}

func newCopyButton(button *widget.Button, text string, onCopied func(string)) *CopyButton {
	b := &CopyButton{
		button:   button,
		copyText: text,
		onCopied: onCopied,
	}
	// ExtendBaseWidget links the embedding struct to the renderer cache; it has
	// to run before anything can ask this widget for its renderer.
	b.ExtendBaseWidget(b)

	b.catcher = &copyCatcher{
		Rectangle: canvas.NewRectangle(color.Transparent),
		owner:     b,
	}
	b.content = container.NewStack(button, b.catcher)

	return b
}

// CreateRenderer makes the button a widget the toolkit draws.
//
// The container is the renderer's object, not the widget itself: the driver
// enters a fyne.Widget or a *fyne.Container, and a struct that embeds a
// container is neither. Without this method every button in the app is a leaf
// the walker cannot enter, and it paints nothing at all: a window full of
// invisible buttons and a blank screen.
func (b *CopyButton) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

// Tapped runs the button. The catcher above the button receives the tap first,
// so this is where a normal press reaches the action the button was built for.
func (b *CopyButton) Tapped(ev *fyne.PointEvent) {
	if b.button.Disabled() {
		return
	}

	b.button.Tapped(ev)
}

// TappedSecondary copies the label. It is the long press on a phone and the
// right-click on a desktop — the same event, and the only gesture that does not
// compete with the tap the button needs or the drag a pane needs to scroll.
func (b *CopyButton) TappedSecondary(*fyne.PointEvent) {
	if b.button.Disabled() || b.copyText == "" {
		return
	}

	if app := fyne.CurrentApp(); app != nil && app.Clipboard() != nil {
		app.Clipboard().SetContent(b.copyText)
	}

	if b.onCopied != nil {
		b.onCopied(b.copyText)
	}
}

// Text is the label the button shows, and what a long press copies unless
// SetCopyText said otherwise.
func (b *CopyButton) Text() string {
	return b.button.Text
}

// SetText changes the label the button shows.
//
// The copy text is deliberately left alone: a phone drops the text of an
// icon-only button, and the label the user long-presses there is the one the
// button was built with, not the empty string it draws.
func (b *CopyButton) SetText(text string) {
	b.button.SetText(text)
}

// SetCopyText changes what a long press copies, for a button whose label
// changes with its state — "More options" and "Fewer options" are one button.
func (b *CopyButton) SetCopyText(text string) {
	b.copyText = text
}

// SetOnTapped replaces the button's tap handler.
//
// It exists because a transcript row reuses one Save button for every save: the
// action is what changes per row, not the widget.
func (b *CopyButton) SetOnTapped(tapped func()) {
	b.button.OnTapped = tapped
}

// SetImportance changes how prominent the button looks, so a tab bar can mark
// the selected tab with the same button widget the others use.
func (b *CopyButton) SetImportance(importance widget.Importance) {
	b.button.Importance = importance
	b.button.Refresh()
}

// Enable makes the button pressable again.
func (b *CopyButton) Enable() {
	b.button.Enable()
}

// Disable stops the button from running its action or copying its label.
func (b *CopyButton) Disable() {
	b.button.Disable()
}

// Disabled reports whether the button is off.
func (b *CopyButton) Disabled() bool {
	return b.button.Disabled()
}

// copyCatcher is the invisible layer that sits above the button.
//
// It has to implement fyne.Tappable as well as fyne.SecondaryTappable: the
// driver keeps the last object under the pointer, so a catcher that knew only
// the secondary gesture would swallow every normal tap on the way to the
// button. Hover is forwarded for the same reason — the catcher is the object
// the pointer meets, and without the forward the button would never highlight.
type copyCatcher struct {
	*canvas.Rectangle

	owner *CopyButton
}

var (
	_ fyne.Tappable          = (*copyCatcher)(nil)
	_ fyne.SecondaryTappable = (*copyCatcher)(nil)
	_ desktop.Hoverable      = (*copyCatcher)(nil)
	_ desktop.Cursorable     = (*copyCatcher)(nil)
)

func (c *copyCatcher) Tapped(ev *fyne.PointEvent) {
	c.owner.Tapped(ev)
}

func (c *copyCatcher) TappedSecondary(ev *fyne.PointEvent) {
	c.owner.TappedSecondary(ev)
}

func (c *copyCatcher) MouseIn(ev *desktop.MouseEvent) {
	c.owner.button.MouseIn(ev)
}

func (c *copyCatcher) MouseMoved(ev *desktop.MouseEvent) {
	c.owner.button.MouseMoved(ev)
}

func (c *copyCatcher) MouseOut() {
	c.owner.button.MouseOut()
}

func (c *copyCatcher) Cursor() desktop.Cursor {
	return c.owner.button.Cursor()
}
