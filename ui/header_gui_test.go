//go:build gui

package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// stopServer is a host the window can hold without listening on anything.
type stopServer struct{}

func (stopServer) Stop() error { return nil }

// The chat header carries what happened — a connection, a message, a refusal —
// and nothing else: no state standing in the line, no discovery chatter (#433).
func TestTheChatHeaderShowsWhatHappened(t *testing.T) {
	g := newScreenGUI(t, false)

	if g.status.Visible() {
		t.Fatal("the header shows a line before anything has happened")
	}

	g.setStatus("Disconnected: the host stopped")

	if !g.status.Visible() || !strings.Contains(g.status.Text(), "host stopped") {
		t.Errorf("the line = %q (visible %v), want the message", g.status.Text(), g.status.Visible())
	}

	g.setStatus("")

	if g.status.Visible() {
		t.Error("an empty status left a line on screen")
	}
}

// The header no longer spends its left side on the app's own name (#345).
func TestTheChatHeaderDoesNotShowTheAppName(t *testing.T) {
	g := newScreenGUI(t, true)
	g.win.Resize(fyne.NewSize(420, 800))
	g.win.Content().Refresh()

	if found := paintedText(g.win.Content(), "cthrone", 0); found != nil {
		t.Error("the chat header still shows the app's name")
	}
}

// inHeader reports whether the chat header paints obj.
func inHeader(g *gui, obj fyne.CanvasObject) bool {
	if g.headerHolder == nil || len(g.headerHolder.Objects) == 0 {
		return false
	}

	var found bool

	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if object == obj {
			found = true

			return
		}

		if part, ok := object.(*fyne.Container); ok {
			for _, child := range part.Objects {
				walk(child)
			}
		}
	}

	walk(g.headerHolder.Objects[0])

	return found
}

// The header holds transcript actions only: Stop moved to Setup, where ending a
// session is as deliberate as starting one (#508).
func TestTheHeaderHasNoStopAndOffersClearAll(t *testing.T) {
	g := newScreenGUI(t, false)

	if inHeader(g, g.stopBtn) {
		t.Error("Stop is still in the chat header")
	}

	if g.stopBtn == nil || !g.stopBtn.Disabled() {
		t.Error("Stop is not on Setup, disabled while nothing is running")
	}

	if !inHeader(g, g.clearAllBtn) {
		t.Error("Clear all is not in the chat header")
	}
}

// Clear all follows the transcript: off while there is nothing to clear.
func TestClearAllIsOffOnAnEmptyTranscript(t *testing.T) {
	g := newScreenGUI(t, false)

	if !g.clearAllBtn.Disabled() {
		t.Error("Clear all is enabled with an empty transcript")
	}

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "hello"}}
	g.mu.Unlock()
	g.updateButtons()

	if g.clearAllBtn.Disabled() {
		t.Error("Clear all is disabled although the transcript has a message")
	}
}

// The selection header acts on the span only: Copy, Delete and Cancel. Clear all
// is the header's own action now (#508).
func TestTheSelectionHeaderKeepsThreeActions(t *testing.T) {
	g := newScreenGUI(t, false)

	if inHeader(g, g.clearAllBtn) == false {
		t.Fatal("Clear all left the header")
	}

	var actions int

	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		switch part := object.(type) {
		case *widgets.CopyButton:
			actions++
		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child)
			}
		}
	}

	walk(g.selectHeader)

	if actions != 3 {
		t.Errorf("the selection header carries %d actions, want Copy, Delete and Cancel", actions)
	}
}

// containsObject reports whether root paints obj, anywhere in its tree.
func containsObject(root fyne.CanvasObject, obj fyne.CanvasObject) bool {
	var found bool

	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if object == obj {
			found = true

			return
		}

		switch part := object.(type) {
		case *fyne.Container:
			for _, child := range part.Objects {
				walk(child)
			}
		case *container.Scroll:
			if part.Content != nil {
				walk(part.Content)
			}
		}
	}

	walk(root)

	return found
}
