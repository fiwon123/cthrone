//go:build gui

package ui

import "testing"

// The header's one icon answers "where does this window stand?" without a
// sentence (#453).
func TestTheStateIcon(t *testing.T) {
	tests := []struct {
		name                            string
		starting, reconnecting, hosting bool
		connected, lost                 bool
		want                            string
	}{
		{"nothing running", false, false, false, false, false, "⚪"},
		{"a host or connect is starting", true, false, false, false, false, "🟡"},
		{"the retry loop is asking", false, true, false, false, true, "🟠"},
		{"this window hosts", false, false, true, false, false, "🟢"},
		{"a client with its peer", false, false, false, true, false, "🔵"},
		{"a session dropped", false, false, false, false, true, "🔴"},
		// A session that is live outranks an older loss, and a host outranks a
		// client: the icon is about now, not about what happened before.
		{"connected after a loss", false, false, false, true, true, "🔵"},
		{"hosting after a loss", false, false, true, false, true, "🟢"},
		// A retry outranks the drop that started it.
		{"reconnecting after a drop", false, true, false, false, true, "🟠"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := stateGlyphFor(tt.starting, tt.reconnecting, tt.hosting, tt.connected, tt.lost)

			if got != tt.want {
				t.Errorf("stateGlyphFor() = %q, want %q", got, tt.want)
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
