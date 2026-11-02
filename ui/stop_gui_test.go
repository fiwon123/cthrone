//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// parentsOf maps every painted object to the object that holds it, so a test can
// ask which row a button is in.
func parentsOf(root fyne.CanvasObject) map[fyne.CanvasObject]fyne.CanvasObject {
	parents := make(map[fyne.CanvasObject]fyne.CanvasObject)

	var walk func(obj fyne.CanvasObject, depth int)
	walk = func(obj fyne.CanvasObject, depth int) {
		if obj == nil || depth > 24 {
			return
		}

		switch co := obj.(type) {
		case *fyne.Container:
			for _, child := range co.Objects {
				parents[child] = obj
				walk(child, depth+1)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(co).Objects() {
				parents[child] = obj
				walk(child, depth+1)
			}
		}
	}

	walk(root, 0)

	return parents
}

// Stop is a connection control, not a message action: a thumb aiming at Send
// used to be able to end the session (#288). It sits in the chat header with the
// other session controls, and Send keeps the composer row.
func TestStopIsInTheHeaderAndNotBesideSend(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)
			g.win.Resize(fyne.NewSize(900, 640))
			g.win.Content().Refresh()

			parents := parentsOf(g.win.Content())

			header, ok := parents[g.stopBtn]
			if !ok {
				t.Fatal("the Stop button is not in the painted tree")
			}

			// The copy confirmation is in the header, which is what makes it a
			// handle on the header without a new field.
			if header != parents[g.copyHint] {
				t.Errorf("Stop is in %T, not in the header with the copy line", header)
			}

			if parents[g.stopBtn] == parents[g.sendBtn] {
				t.Error("Stop is still in the composer row beside Send")
			}

			if parents[g.attachBtn] == parents[g.stopBtn] {
				t.Error("Stop is still beside Attach")
			}
		})
	}
}
