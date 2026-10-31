//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// These tests pin the other half of #186: a wrapper widget must paint where it
// is hit-tested.
//
// Fyne computes an object's absolute position as the parent's plus the object's
// own Position(). A wrapper widget that embeds a *fyne.Container and hands that
// same container back from CreateRenderer therefore has its position counted
// twice — once as the widget and once as the renderer object — so its children
// paint at hitPosition + widgetPosition. Tapping the visible label then lands
// on the next widget whose hit region covers that point.
//
// The v0.8.4 desktop Setup form is the reproduction: the Setup tab is painted
// at x≈165 while it reacts at x≈89, so clicking the visible "Setup" opened
// "Devices". The tests below tap where each widget is *painted*, and assert the
// widget it drew acts.
//
// absolutePosition mirrors internal/driver.walkObjectTree on purpose: that walk
// is the one both the painter and the hit test use, and the point of the test is
// that they agree. It is not a second driver.

// absolutePosition returns the position the driver's walk computes for target.
func absolutePosition(t *testing.T, root, target fyne.CanvasObject) fyne.Position {
	t.Helper()

	var (
		found fyne.Position
		ok    bool
	)

	var walk func(object fyne.CanvasObject, offset fyne.Position)
	walk = func(object fyne.CanvasObject, offset fyne.Position) {
		if ok || !object.Visible() {
			return
		}

		pos := offset.Add(object.Position())
		if object == target {
			found, ok = pos, true

			return
		}

		switch node := object.(type) {
		case *fyne.Container:
			for _, child := range node.Objects {
				walk(child, pos)
			}
		case fyne.Widget:
			if renderer := test.WidgetRenderer(node); renderer != nil {
				for _, child := range renderer.Objects() {
					walk(child, pos)
				}
			}
		}
	}

	walk(root, fyne.NewPos(0, 0))

	if !ok {
		t.Fatalf("%T is not in the visible tree", target)
	}

	return found
}

// paintedCentre is the centre of the area a wrapper widget draws: where the
// walk puts its renderer's content, plus half the widget's size.
func paintedCentre(t *testing.T, root fyne.CanvasObject, wrapper fyne.Widget) fyne.Position {
	t.Helper()

	base := absolutePosition(t, root, wrapper)
	content := test.WidgetRenderer(wrapper).Objects()[0]
	x := base.X + content.Position().X + wrapper.Size().Width/2
	y := base.Y + content.Position().Y + wrapper.Size().Height/2

	return fyne.NewPos(x, y)
}

// TestTabsOpenWhereTheyArePainted taps the centre of each tab label as it is
// drawn. Every screen must open the tab under the fingers.
func TestTabsOpenWhereTheyArePainted(t *testing.T) {
	tests := []struct {
		name   string
		mobile bool
		size   fyne.Size
	}{
		{"desktop", false, fyne.NewSize(900, 640)},
		{"phone", true, fyne.NewSize(360, 640)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newScreenGUI(t, tt.mobile)
			g.win.Resize(tt.size)

			root := g.win.Content()
			for i, item := range g.tabs.Items {
				g.tabs.SelectIndex(0)

				tap := paintedCentre(t, root, g.tabs.ButtonAt(i))
				test.TapCanvas(g.win.Canvas(), tap)

				if got := g.tabs.SelectedIndex(); got != i {
					t.Errorf("tapping the painted %q tab selected tab %d, want %d (tap at %v)", item.Text, got, i, tap)
				}
			}
		})
	}
}

// TestWrapperContentSitsAtTheWidget is the contract the taps rely on: the
// renderer's content begins at the widget's own origin, so the driver's walk
// applies the widget's position exactly once. Each wrapper type is checked on
// the Setup tab, where v0.8.4 drew its buttons up to 39 px below their hit
// regions.
func TestWrapperContentSitsAtTheWidget(t *testing.T) {
	g := newScreenGUI(t, false)
	g.win.Resize(fyne.NewSize(900, 640))
	g.tabs.SelectIndex(1)
	g.win.Resize(fyne.NewSize(900, 640))

	root := g.win.Content()
	wrappers := map[string]fyne.Widget{
		"tab bar":   g.tabs,
		"button":    g.moreBtn,
		"check row": g.tlsCheck,
	}

	for name, wrapper := range wrappers {
		base := absolutePosition(t, root, wrapper)
		content := test.WidgetRenderer(wrapper).Objects()[0]

		if painted := base.Add(content.Position()); painted != base {
			t.Errorf("%s: content is painted at %v but the widget reacts at %v", name, painted, base)
		}
	}
}

// TestSetupButtonsArePaintedInsideTheWindow keeps the two actions on the
// Setup tab reachable. The second column of the action grid used to be drawn
// off the right edge of a 900 px window, so "Connect" was invisible and could
// not be tapped where it appeared.
func TestSetupButtonsArePaintedInsideTheWindow(t *testing.T) {
	g := newScreenGUI(t, false)
	g.win.Resize(fyne.NewSize(900, 640))
	g.tabs.SelectIndex(1)
	g.win.Resize(fyne.NewSize(900, 640))

	root := g.win.Content()
	width := g.win.Canvas().Size().Width

	buttons := map[string]*widgets.CopyButton{
		"Host":    g.hostBtn,
		"Connect": g.connectBtn,
		"More":    g.moreBtn,
	}

	for name, button := range buttons {
		base := absolutePosition(t, root, button)
		offset := test.WidgetRenderer(button).Objects()[0].Position()
		left := base.X + offset.X
		right := left + button.Size().Width

		if left < 0 || right > width {
			t.Errorf("the %s button is painted at x=%v..%v, outside the %v px window", name, left, right, width)
		}
	}
}
