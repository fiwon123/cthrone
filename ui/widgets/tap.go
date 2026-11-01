package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// tapCatcher is the transparent layer that carries a Text's tap and long press
// above its label.
//
// It exists because of how Fyne chooses a gesture target: the driver dispatches
// to the deepest visible object under the pointer that implements the gesture,
// and a selectable label keeps its own selection child deeper than the widget
// wrapping it. On a desktop that child takes the click and then does nothing
// with it off-mobile — `selectable.Tapped` starts with
// `if !fyne.CurrentDevice().IsMobile() { return }` — so the row's handler is
// never reached and the row looks dead (#280).
//
// Drawn last in a stack, this layer is the deepest object, so it wins the hit
// test. It must not take either gesture that Text deliberately does not take:
// being neither Draggable nor Touchable keeps a desktop drag on the selection
// beneath it and a phone drag on the scrolling pane.
//
// It is hidden until its Text has something to do, so a plain line of text adds
// no gesture of its own and Fyne's own selection and copy menu stay in charge.
type tapCatcher struct {
	widget.BaseWidget

	onTapped    func(*fyne.PointEvent)
	onSecondary func(*fyne.PointEvent)
}

// newTapCatcher builds the layer and wires it to the Text's handlers.
func newTapCatcher(onTapped, onSecondary func(*fyne.PointEvent)) *tapCatcher {
	catcher := &tapCatcher{onTapped: onTapped, onSecondary: onSecondary}
	catcher.ExtendBaseWidget(catcher)

	return catcher
}

// CreateRenderer draws nothing: the layer has to be under the pointer, not
// under the reader's eye. A transparent rectangle still gives the widget a
// renderer, and the walk never picks the rectangle over the widget itself.
func (c *tapCatcher) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// Tapped is a plain tap, forwarded to the text's own action.
func (c *tapCatcher) Tapped(ev *fyne.PointEvent) {
	if c.onTapped == nil {
		return
	}

	c.onTapped(ev)
}

// TappedSecondary is the copy gesture: a long press on a touch screen, a right
// click on a desktop.
func (c *tapCatcher) TappedSecondary(ev *fyne.PointEvent) {
	if c.onSecondary == nil {
		return
	}

	c.onSecondary(ev)
}
