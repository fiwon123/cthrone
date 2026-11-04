//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	drivermobile "fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/ui/widgets"
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

	want := g.messages[0].body()

	row := paintedText(g.win.Content(), want, 0)
	if row == nil {
		t.Fatal("the chat row is not in the painted tree at all")
	}

	return g, row
}

// Tapping a chat row no longer copies: a copy is asked for — the row's own
// button, the header's Copy all, or by selecting the text — and a tap is a place
// to start a selection instead (#496). The click is delivered the way the
// toolkit delivers one, so this also proves the row is still the tap target
// (#280).
func TestTappingAChatRowCopiesNothing(t *testing.T) {
	g, row := chatRowWithMessage(t, false)

	g.app.Clipboard().SetContent("unchanged")
	tapFromTheToolkit(t, g.win.Content(), row)

	if got := g.app.Clipboard().Content(); got != "unchanged" {
		t.Errorf("a tap on a row copied %q, want nothing", got)
	}
}

// The tap still marks a range: with Select on, the row is the handle a thumb can
// hit, and removing the copy must not take that with it (#496).
func TestATapStillMarksARangeWhileSelecting(t *testing.T) {
	g, row := chatRowWithMessage(t, false)

	g.beginSelection()
	tapFromTheToolkit(t, g.win.Content(), row)

	if g.sel.anchor != 0 {
		t.Errorf("the tap marked anchor %d, want the first message", g.sel.anchor)
	}
}

// rowCopyButton finds the copy button on a row: the button that is not the eye.
func rowCopyButton(t *testing.T, row fyne.CanvasObject) *widgets.CopyButton {
	t.Helper()

	eye := rowReveal(t, row)

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want a container", row)
	}

	caption, ok := box.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("row[0] is %T, want the caption cell", box.Objects[0])
	}

	var found *widgets.CopyButton

	var walk func(object fyne.CanvasObject)

	walk = func(object fyne.CanvasObject) {
		switch part := object.(type) {
		case *widgets.CopyButton:
			if part != eye {
				found = part
			}
		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child)
			}
		}
	}

	walk(caption)

	if found == nil {
		t.Fatal("the row carries no copy button")
	}

	return found
}

// Every row copies its own message, and only its own (#496).
func TestEachRowCopiesItsOwnMessage(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "the first"}, {from: "laptop", text: "the second"}}
	g.mu.Unlock()

	for id := range g.messages {
		want := g.messages[id].copyText()

		row := g.chatList.CreateItem()
		g.updateChatRow(id, row)

		g.app.Clipboard().SetContent("")
		test.Tap(rowCopyButton(t, row))

		if got := g.app.Clipboard().Content(); got != want {
			t.Errorf("row %d copied %q, want %q", id, got, want)
		}
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

// With the keyboard up on a phone, a tap on the transcript puts the keyboard away
// instead of copying: the tap was meant for Send, and the window moves under it
// (#325).
func TestATapWithTheKeyboardUpDoesNotCopy(t *testing.T) {
	g, _ := chatRowWithMessage(t, true)

	g.win.Canvas().Focus(g.entry)

	before := g.app.Clipboard().Content()
	g.selectChatRow(0)

	if after := g.app.Clipboard().Content(); after != before {
		t.Errorf("a tap with the keyboard up copied %q", after)
	}

	if g.composerHasKeyboard() {
		t.Error("the tap left the keyboard up; the next tap cannot then be deliberate")
	}
}

// On a desktop there is no keyboard to put away: a click on a row still copies.
func TestAClickOnARowCopiesNothingEvenWithTheComposerFocused(t *testing.T) {
	g, _ := chatRowWithMessage(t, false)

	g.win.Canvas().Focus(g.entry)
	g.app.Clipboard().SetContent("unchanged")

	g.selectChatRow(0)

	if got := g.app.Clipboard().Content(); got != "unchanged" {
		t.Errorf("a click on a row copied %q with the composer focused, want nothing", got)
	}
}
