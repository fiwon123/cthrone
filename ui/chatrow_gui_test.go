//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
)

// Tapping a chat row copies the line: the gesture the transcript teaches, and
// the reason the row carries a tap at all (#231).
//
// The click is delivered the way the toolkit delivers one. On a desktop the
// row's selectable label keeps a child deeper than the widget that binds the
// tap, so the click used to die there and the clipboard stayed empty (#280).
func TestChatRowCopiesWhenClickedOnADesktop(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{text: "hello there", from: "laptop"}}
	g.mu.Unlock()

	g.chatList.Refresh()
	g.win.Resize(fyne.NewSize(900, 640))
	g.win.Content().Refresh()

	want := g.messages[0].line()

	row := paintedRow(g.win.Content(), want, 0)
	if row == nil {
		t.Fatal("the chat row is not in the painted tree at all")
	}

	g.app.Clipboard().SetContent("")

	tapFromTheToolkit(t, g.win.Content(), row)

	if got := g.app.Clipboard().Content(); got != want {
		t.Errorf("clicking a chat row copied %q, want %q", got, want)
	}
}
