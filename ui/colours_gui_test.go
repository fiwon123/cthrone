//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// senderLine finds the coloured sender line of a chat row.
func senderLine(t *testing.T, row fyne.CanvasObject) *widgets.Text {
	t.Helper()

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want *fyne.Container", row)
	}

	caption, ok := box.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("row[0] is %T, want the caption cell", box.Objects[0])
	}

	var found *widgets.Text

	for _, object := range caption.Objects {
		part, ok := object.(*fyne.Container)
		if !ok {
			continue
		}

		for _, child := range part.Objects {
			if inner, ok := child.(*fyne.Container); ok {
				for _, item := range inner.Objects {
					if text, ok := item.(*widgets.Text); ok {
						found = text
					}
				}
			}
		}
	}

	if found == nil {
		t.Fatal("the row has no sender line")
	}

	return found
}

// A sender keeps its colour: the name decides it, so the same device is the same
// colour in every window and after a restart (#346).
func TestASendersColourIsStable(t *testing.T) {
	if senderColour("laptop") != senderColour("laptop") {
		t.Error("the same name got two colours")
	}

	// The palette is small, so two names may share a colour once a room is
	// larger than it: the name still says who it is, and the colour is a help,
	// not the only cue. What it must do is spread names over the palette.
	seen := map[interface{}]bool{}
	for _, name := range []string{"laptop", "phone", "tablet", "desk", "kitchen"} {
		seen[senderColour(name)] = true
	}

	if len(seen) < 2 {
		t.Errorf("every name got the same colour (%d distinct); the colour says nothing", len(seen))
	}
}

// The row says who spoke on its own line, in that colour, and the message body
// does not repeat the name (#346).
func TestTheRowShowsTheSenderApartFromTheMessage(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop (192.168.1.20:8080)", text: "hello there"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	sender := senderLine(t, row)
	if sender.Text() != "laptop (192.168.1.20:8080)" {
		t.Errorf("the sender line reads %q, want the name and address", sender.Text())
	}

	if sender.Importance() != senderImportance("laptop (192.168.1.20:8080)", false) {
		t.Error("the sender line is not the colour the name decides")
	}

	// The name is text a reader may copy, so it takes a selection: a desktop
	// selects it directly, a phone by a long press (#503).
	if !sender.Selectable() {
		t.Error("the sender's name cannot be selected")
	}

	label, _, _, _, _ := chatRowObjects(t, row)
	if got := label.Text(); got != "hello there" {
		t.Errorf("the caption reads %q, want the message alone", got)
	}

	// The clipboard keeps the text alone: the sender is the row's business
	// (#474).
	if got, want := g.messages[0].copyText(), "hello there"; got != want {
		t.Errorf("copyText() = %q, want %q", got, want)
	}
}

// What this device said is "You", and it gets its own colour like anyone else.
func TestOurOwnMessagesSayYou(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{text: "mine"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	if got := senderLine(t, row).Text(); got != "You" {
		t.Errorf("our own sender line reads %q, want You", got)
	}
}
