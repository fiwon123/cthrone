//go:build gui

package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// stopServer is a host the window can hold without listening on anything.
type stopServer struct{}

func (stopServer) Stop() error { return nil }

// The chat header says where the window stands — offline, hosting, connected to
// the device the handshake named — with a mark whose colour follows (#345).
func TestTheChatHeaderSaysWhereWeAre(t *testing.T) {
	g := newScreenGUI(t, false)

	if got := g.status.Text(); got != "● offline" {
		t.Errorf("with no session the header says %q, want the offline mark", got)
	}

	if got := g.status.Importance(); got != widget.LowImportance {
		t.Errorf("the offline state has importance %v, want the low one", got)
	}

	g.mu.Lock()
	g.server = stopServer{}
	g.mu.Unlock()

	g.refreshConnectionState()

	if got := g.status.Text(); got != "● hosting" {
		t.Errorf("while hosting the header says %q, want hosting", got)
	}

	if got := g.status.Importance(); got != widget.SuccessImportance {
		t.Errorf("the hosting state has importance %v, want the success one", got)
	}

	// A client, with the name the handshake authenticated.
	g.mu.Lock()
	g.server = nil
	g.sess = newSendRecorder()
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.status.Text(); got != "● connected" {
		t.Errorf("a connected window says %q, want connected", got)
	}

	g.mu.Lock()
	g.peerName = "laptop"
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.status.Text(); !strings.Contains(got, "laptop") {
		t.Errorf("the header says %q, want the peer's name", got)
	}

	// A handshake in flight is its own state, not "offline".
	g.mu.Lock()
	g.sess, g.peerName, g.starting = nil, "", true
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.status.Text(); !strings.Contains(got, "connecting") {
		t.Errorf("while starting the header says %q, want connecting", got)
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

// The header's one line is the status: where the window stands until something
// newer happens, and the message itself while it lasts (#364, #383).
func TestTheStatusLineFallsBackToTheConnectionState(t *testing.T) {
	g := newScreenGUI(t, false)

	if !g.status.Visible() {
		t.Fatal("the header's line is hidden while the window has a state to show")
	}

	if got := g.status.Text(); got != "● offline" {
		t.Errorf("with nothing to say the line shows %q, want the offline state", got)
	}

	g.setStatus("Selected 192.168.1.20")

	if got := g.status.Text(); got != "Selected 192.168.1.20" {
		t.Errorf("a message did not take the line: %q", got)
	}

	// A state change does not steal a message that is still news.
	g.mu.Lock()
	g.server = stopServer{}
	g.mu.Unlock()

	g.refreshConnectionState()

	if got := g.status.Text(); got != "Selected 192.168.1.20" {
		t.Errorf("the state overwrote a message: %q", got)
	}

	g.setStatus("")

	if got := g.status.Text(); got != "● hosting" {
		t.Errorf("a cleared status shows %q, want the state it was hiding", got)
	}
}
