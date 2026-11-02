//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// The icon and its one word answer "where does this window stand?" (#453,
// #464).
func TestTheStateIcon(t *testing.T) {
	tests := []struct {
		name                            string
		starting, reconnecting, hosting bool
		connected, lost                 bool
		want                            string
		word                            string
	}{
		{"nothing running", false, false, false, false, false, "⚪", "offline"},
		{"a host or connect is starting", true, false, false, false, false, "🟡", "starting"},
		{"the retry loop is asking", false, true, false, false, true, "🟠", "reconnecting"},
		{"this window hosts", false, false, true, false, false, "🟢", "hosting"},
		{"a client with its peer", false, false, false, true, false, "🔵", "connected"},
		{"a session dropped", false, false, false, false, true, "🔴", "lost"},
		// A session that is live outranks an older loss, and a host outranks a
		// client: the icon is about now, not about what happened before.
		{"connected after a loss", false, false, false, true, true, "🔵", "connected"},
		{"hosting after a loss", false, false, true, false, true, "🟢", "hosting"},
		// A retry outranks the drop that started it.
		{"reconnecting after a drop", false, true, false, false, true, "🟠", "reconnecting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, word, _ := stateGlyphFor(tt.starting, tt.reconnecting, tt.hosting, tt.connected, tt.lost)

			if got != tt.want {
				t.Errorf("stateGlyphFor() = %q, want %q", got, tt.want)
			}

			if word != tt.word {
				t.Errorf("stateGlyphFor() word = %q, want %q", word, tt.word)
			}
		})
	}
}

// The icon is redrawn from the window's state: a Stop is offline, not lost.
func TestTheStateIconFollowsStopAndLoss(t *testing.T) {
	g := newScreenGUI(t, false)

	if g.stateGlyph.Text != "⚪" {
		t.Errorf("a fresh window shows %q, want offline", g.stateGlyph.Text)
	}

	g.mu.Lock()
	g.sess = newSendRecorder()
	g.lost = true
	g.mu.Unlock()

	g.refreshStateGlyph()

	if g.stateGlyph.Text != "🔵" {
		t.Errorf("a live session shows %q, want connected", g.stateGlyph.Text)
	}

	g.mu.Lock()
	g.sess = nil
	g.mu.Unlock()

	g.refreshStateGlyph()

	if g.stateGlyph.Text != "🔴" {
		t.Errorf("a lost session shows %q, want lost", g.stateGlyph.Text)
	}

	g.stop()

	if g.stateGlyph.Text != "⚪" {
		t.Errorf("after Stop the window shows %q, want offline", g.stateGlyph.Text)
	}
}

// The status line is under the transcript, not in the header, and a tap on it
// copies what it says — a message a user wants to quote, or report (#457).
func TestTheStatusLineIsBelowAndCopiesOnATap(t *testing.T) {
	g := newScreenGUI(t, false)
	g.win.Content().Refresh()

	const message = "No host answered on this network"
	g.setStatus(message)

	if !g.status.Visible() {
		t.Fatal("the status line is hidden although it has something to say")
	}

	test.Tap(g.status)

	if got := g.app.Clipboard().Content(); got != message {
		t.Errorf("the clipboard holds %q, want the status line", got)
	}
}

// The header carries the state icon and the actions, and no text (#457).
func TestTheHeaderCarriesNoText(t *testing.T) {
	g := newScreenGUI(t, false)
	g.win.Content().Refresh()

	header := parentsOf(g.win.Content())[g.stateGlyph]
	if header == nil {
		t.Fatal("the state icon is not painted")
	}

	if parentsOf(g.win.Content())[g.stopBtn] != header {
		t.Error("Stop is not in the same row as the state icon")
	}

	if parentsOf(g.win.Content())[g.status] == header {
		t.Error("the status line is in the header")
	}
}
