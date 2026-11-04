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
func TestStopIsOnSetupAndNotBesideSend(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)
			g.win.Resize(fyne.NewSize(900, 640))
			g.win.Content().Refresh()

			// Stop ends a session, so it belongs with Host and Connect on Setup,
			// not in the chat header a thumb reaches while reading (#508).
			if inHeader(g, g.stopBtn) {
				t.Fatal("Stop is in the chat header")
			}

			parents := parentsOf(g.win.Content())

			header, ok := parents[g.stopBtn]
			if !ok {
				t.Fatal("the Stop button is not in the painted tree")
			}

			if header == parents[g.stateGlyph] {
				t.Error("Stop is still in the state's row")
			}

			if header == parents[g.sendBtn] {
				t.Error("Stop is beside Send")
			}

			// It is inside the Setup tab, with the two buttons that start a
			// session.
			setup := g.tabs.Items[1].Content
			if !containsObject(setup, g.stopBtn) {
				t.Error("Stop is on no tab at all")
			}
		})
	}
}
