//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	drivermobile "fyne.io/fyne/v2/driver/mobile"
)

// chatRowWithMessage puts one line in the transcript and returns the text
// object the toolkit paints for it.
func chatRowWithMessage(t *testing.T, mobile bool) (*gui, fyne.CanvasObject) {
	t.Helper()

	g := newScreenGUI(t, mobile)

	g.mu.Lock()
	g.messages = []chatMessage{{text: "hello there", from: "laptop"}}
	g.mu.Unlock()

	g.chatList.Refresh()
	g.win.Resize(fyne.NewSize(900, 640))
	g.win.Content().Refresh()

	want := g.messages[0].line()

	row := paintedText(g.win.Content(), want, 0)
	if row == nil {
		t.Fatal("the chat row is not in the painted tree at all")
	}

	return g, row
}

// Tapping a chat row copies the line: the gesture the transcript teaches, and
// the reason the row carries a tap at all (#231).
//
// The click is delivered the way the toolkit delivers one. On a desktop the
// row's selectable label keeps a child deeper than the widget that binds the
// tap, so the click used to die there and the clipboard stayed empty (#280).
func TestChatRowCopiesWhenClickedOnADesktop(t *testing.T) {
	g, row := chatRowWithMessage(t, false)

	want := g.messages[0].line()
	g.app.Clipboard().SetContent("")

	tapFromTheToolkit(t, g.win.Content(), row)

	if got := g.app.Clipboard().Content(); got != want {
		t.Errorf("clicking a chat row copied %q, want %q", got, want)
	}
}

// The layer that carries the tap must not carry the drag. On a desktop a drag
// still has to reach the label's own selection — that is how a mouse selects
// text — and on a phone it still has to reach the pane, or the transcript cannot
// be scrolled by touch (#280).
func TestTheTapLayerLeavesTheDragAlone(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g, row := chatRowWithMessage(t, mobile)

			start, ok := canvasPosition(g.win.Content(), row)
			if !ok {
				t.Fatal("the chat row has no position")
			}

			centre := start.Add(fyne.NewPos(row.Size().Width/2, row.Size().Height/2))

			tapTarget, _ := hitTest(g.win.Content(), centre)

			// Each driver picks the drag target from the interfaces it knows:
			// the desktop one from Draggable, the mobile one from Draggable and
			// then Touchable.
			dragged, _ := hitTestMatching(g.win.Content(), centre, func(obj fyne.CanvasObject) bool {
				if _, draggable := obj.(fyne.Draggable); draggable {
					return true
				}

				if !mobile {
					return false
				}

				_, touchable := obj.(drivermobile.Touchable)

				return touchable
			})

			if dragged == nil {
				t.Fatal("nothing under the row takes a drag, so its text can neither be selected nor scrolled")
			}

			// One object must not take both: a layer that took the drag as well
			// as the tap would stop the selection or the scroll.
			if dragged == tapTarget {
				t.Fatalf("%T takes both the tap and the drag", dragged)
			}

			if mobile {
				if !treeContains(dragged, row) {
					t.Fatalf("a phone drag lands on %T inside the row, so the transcript would not scroll", dragged)
				}

				return
			}

			if dragged == fyne.CanvasObject(row) || !treeContains(row, dragged) {
				t.Fatalf("a desktop drag lands on %T, not on the row's selection", dragged)
			}
		})
	}
}

// A long press on a phone still copies the line, through the layer: the press
// lands on the layer, which hands it to the text, which brings Fyne's selection
// up and offers Copy.
func TestAChatRowStillCopiesOnAPhoneThroughTheTapLayer(t *testing.T) {
	g, row := chatRowWithMessage(t, true)

	start, ok := canvasPosition(g.win.Content(), row)
	if !ok {
		t.Fatal("the chat row has no position")
	}

	centre := start.Add(fyne.NewPos(row.Size().Width/2, row.Size().Height/2))

	found, _ := hitTest(g.win.Content(), centre)

	press, ok := found.(fyne.SecondaryTappable)
	if !ok {
		t.Fatalf("a long press on the row lands on %T, which does not take one", found)
	}

	press.TappedSecondary(&fyne.PointEvent{Position: centre, AbsolutePosition: centre})

	if g.win.Canvas().Overlays().Top() == nil {
		t.Error("the long press offered no Copy menu")
	}
}
