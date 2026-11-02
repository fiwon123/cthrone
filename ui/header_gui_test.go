//go:build gui

package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
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
