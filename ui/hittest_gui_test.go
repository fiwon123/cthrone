//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"
)

// hitTest answers the question a tap really asks: which object would the driver
// hand this point to?
//
// It is the walk the drivers run (internal/driver.FindObjectAtPositionMatching
// over WalkVisibleObjectTree): parent first, then each child in order, skipping
// anything not visible, and the *last* match wins — so the winner is the
// deepest, latest child that takes the gesture. Widgets contribute their
// renderer's objects as children, which is how a wrapper's internal selection
// child can beat the wrapper itself.
//
// It is stated here rather than imported because the drivers keep it internal.
// What it buys is the one thing test.Tap cannot do: test.Tap calls a widget's
// handler directly, so it proves the handler works and not that a click on the
// row ever reaches it (#280).
func hitTest(root fyne.CanvasObject, point fyne.Position) (fyne.CanvasObject, fyne.Position) {
	// The same four interfaces the mobile driver matches, so a hit here is a hit
	// there; the desktop driver matches a superset that begins with these.
	return hitTestMatching(root, point, func(obj fyne.CanvasObject) bool {
		switch obj.(type) {
		case fyne.Tappable, fyne.SecondaryTappable, mobile.Touchable, fyne.DoubleTappable:
			return true
		}

		return false
	})
}

// hitTestMatching is hitTest for any gesture, so a test can ask the related
// question too: with a tap layer in place, does a *drag* still reach the object
// that has to make the selection (a desktop) or scroll the pane (a phone)?
func hitTestMatching(root fyne.CanvasObject, point fyne.Position, matches func(fyne.CanvasObject) bool) (fyne.CanvasObject, fyne.Position) {
	var found fyne.CanvasObject
	var foundPos fyne.Position

	var walk func(obj fyne.CanvasObject, offset fyne.Position)
	walk = func(obj fyne.CanvasObject, offset fyne.Position) {
		if obj == nil || !obj.Visible() {
			return
		}

		pos := obj.Position().Add(offset)
		if point.X < pos.X || point.Y < pos.Y ||
			point.X >= pos.X+obj.Size().Width || point.Y >= pos.Y+obj.Size().Height {
			return
		}

		if matches(obj) {
			found = obj
			foundPos = fyne.NewPos(point.X-pos.X, point.Y-pos.Y)
		}

		switch co := obj.(type) {
		case *fyne.Container:
			for _, child := range co.Objects {
				walk(child, pos)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(co).Objects() {
				walk(child, pos)
			}
		}
	}

	walk(root, fyne.NewPos(0, 0))

	return found, foundPos
}

// canvasPosition is where obj is drawn on the canvas.
func canvasPosition(root, want fyne.CanvasObject) (fyne.Position, bool) {
	var found fyne.Position
	ok := false

	var walk func(obj fyne.CanvasObject, offset fyne.Position)
	walk = func(obj fyne.CanvasObject, offset fyne.Position) {
		if obj == nil || ok {
			return
		}

		pos := obj.Position().Add(offset)
		if obj == want {
			found = pos
			ok = true

			return
		}

		switch co := obj.(type) {
		case *fyne.Container:
			for _, child := range co.Objects {
				walk(child, pos)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(co).Objects() {
				walk(child, pos)
			}
		}
	}

	walk(root, fyne.NewPos(0, 0))

	return found, ok
}

// tapFromTheToolkit is a click on the middle of obj, delivered the way the
// toolkit delivers one: the object the driver picks is the object that runs.
//
// The tests drive this instead of test.Tap so that a row a user cannot click
// fails the test. test.Tap calls a widget's handler directly, which proves the
// handler works and says nothing about whether a click ever reaches it — the
// distinction #280 exists for.
func tapFromTheToolkit(t *testing.T, root fyne.CanvasObject, obj fyne.CanvasObject) fyne.CanvasObject {
	t.Helper()

	pos, ok := canvasPosition(root, obj)
	if !ok {
		t.Fatalf("the row %T is not in the painted tree", obj)
	}

	centre := pos.Add(fyne.NewPos(obj.Size().Width/2, obj.Size().Height/2))

	found, _ := hitTest(root, centre)
	if found == nil {
		t.Fatalf("nothing at %v takes a tap — the row at %v cannot be clicked", centre, pos)
	}

	// A double-tappable winner waits for the second click first; either way the
	// tap is what runs when the window closes, and that is what is under test.
	if tapped, ok := found.(fyne.Tappable); ok {
		tapped.Tapped(&fyne.PointEvent{Position: centre, AbsolutePosition: centre})
	}

	return found
}

// treeContains reports whether want is root or is drawn inside it.
func treeContains(root, want fyne.CanvasObject) bool {
	if root == nil || want == nil {
		return false
	}

	if root == want {
		return true
	}

	switch co := root.(type) {
	case *fyne.Container:
		for _, child := range co.Objects {
			if treeContains(child, want) {
				return true
			}
		}
	case fyne.Widget:
		for _, child := range test.WidgetRenderer(co).Objects() {
			if treeContains(child, want) {
				return true
			}
		}
	}

	return false
}
