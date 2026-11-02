//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// findButton is a depth-bounded walk for the button whose label is label. A
// viewer is an overlay, so it is not in the window's content tree; the walk
// starts at the overlay the toolkit holds.
func findButton(t *testing.T, root fyne.CanvasObject, label string) *widgets.CopyButton {
	t.Helper()

	var found *widgets.CopyButton

	var walk func(object fyne.CanvasObject, depth int)
	walk = func(object fyne.CanvasObject, depth int) {
		if object == nil || depth > 12 || found != nil {
			return
		}

		switch part := object.(type) {
		case *widgets.CopyButton:
			if part.Text() == label {
				found = part
			}

		case fyne.Widget:
			for _, child := range test.WidgetRenderer(part).Objects() {
				walk(child, depth+1)
			}

		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child, depth+1)
			}
		}
	}

	walk(root, 0)

	if found == nil {
		t.Fatalf("no %q button in the viewer", label)
	}

	return found
}

// The viewer is dismissed by a tap outside the picture. It used to be a modal
// dialog, where a tap outside did nothing and the only way out was the Close
// button — the far corner of a phone screen (#387).
func TestTheImageViewerClosesOnATapAnywhereElse(t *testing.T) {
	g := newScreenGUI(t, false)

	g.showImage(writeTestPNG(t))

	overlays := g.win.Canvas().Overlays()
	if len(overlays.List()) == 0 {
		t.Fatal("opening a picture showed no viewer")
	}

	outside, ok := overlays.Top().(fyne.Tappable)
	if !ok {
		t.Fatalf("the viewer's backdrop is %T, which takes no tap", overlays.Top())
	}

	test.Tap(outside)

	if left := len(overlays.List()); left != 0 {
		t.Errorf("a tap outside the picture left %d overlay(s) open, want none", left)
	}
}

// Close still closes: the desktop's mouse, a keyboard user and the tests all
// use it.
func TestTheImageViewerClosesWithItsButton(t *testing.T) {
	g := newScreenGUI(t, false)

	g.showImage(writeTestPNG(t))

	overlays := g.win.Canvas().Overlays()
	if len(overlays.List()) == 0 {
		t.Fatal("opening a picture showed no viewer")
	}

	closeBtn := findButton(t, overlays.Top(), "Close")
	test.Tap(closeBtn)

	if left := len(overlays.List()); left != 0 {
		t.Errorf("Close left %d overlay(s) open, want none", left)
	}
}
