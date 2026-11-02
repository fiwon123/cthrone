//go:build gui

package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// senderLine finds the coloured sender line of a chat row.
func senderLine(t *testing.T, row fyne.CanvasObject) *canvas.Text {
	t.Helper()

	var found *canvas.Text

	var walk func(object fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		switch part := object.(type) {
		case *canvas.Text:
			found = part
		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child)
			}
		}
	}

	walk(row)

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
	if sender.Text != "laptop (192.168.1.20:8080)" {
		t.Errorf("the sender line reads %q, want the name and address", sender.Text)
	}

	if sender.Color != senderColour("laptop (192.168.1.20:8080)") {
		t.Error("the sender line is not the colour the name decides")
	}

	label, _, _, _, _ := chatRowObjects(t, row)
	if got := label.Text(); got != "hello there" {
		t.Errorf("the caption reads %q, want the message alone", got)
	}

	// The clipboard keeps the sender: a copied line with no name is useless.
	if got, want := g.messages[0].line(), "laptop (192.168.1.20:8080): hello there"; got != want {
		t.Errorf("line() = %q, want %q", got, want)
	}

	if !strings.Contains(g.messages[0].line(), g.messages[0].senderDisplay()) {
		t.Error("the copied line does not name the sender")
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

	if got := senderLine(t, row).Text; got != "You" {
		t.Errorf("our own sender line reads %q, want You", got)
	}
}
