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

	if got := g.stateText.Text(); got != "● offline" {
		t.Errorf("with no session the header says %q, want the offline mark", got)
	}

	if got := g.stateText.Importance(); got != widget.LowImportance {
		t.Errorf("the offline state has importance %v, want the low one", got)
	}

	g.mu.Lock()
	g.server = stopServer{}
	g.mu.Unlock()

	g.refreshConnectionState()

	if got := g.stateText.Text(); got != "● hosting" {
		t.Errorf("while hosting the header says %q, want hosting", got)
	}

	if got := g.stateText.Importance(); got != widget.SuccessImportance {
		t.Errorf("the hosting state has importance %v, want the success one", got)
	}

	// A client, with the name the handshake authenticated.
	g.mu.Lock()
	g.server = nil
	g.sess = newSendRecorder()
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.stateText.Text(); got != "● connected" {
		t.Errorf("a connected window says %q, want connected", got)
	}

	g.mu.Lock()
	g.peerName = "laptop"
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.stateText.Text(); !strings.Contains(got, "laptop") {
		t.Errorf("the header says %q, want the peer's name", got)
	}

	// A handshake in flight is its own state, not "offline".
	g.mu.Lock()
	g.sess, g.peerName, g.starting = nil, "", true
	g.mu.Unlock()

	g.refreshConnectionState()
	if got := g.stateText.Text(); !strings.Contains(got, "connecting") {
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

// The status line under the header exists only while it has something to say: a
// permanent sentence there was the report (#364).
func TestTheStatusLineHidesWhenItIsEmpty(t *testing.T) {
	g := newScreenGUI(t, false)

	if g.status.Visible() {
		t.Error("the status line is showing before anything has happened")
	}

	g.setStatus("Selected 192.168.1.20")

	if !g.status.Visible() {
		t.Error("a status message did not show the line")
	}

	g.setStatus("")

	if g.status.Visible() {
		t.Error("an empty status left the line showing")
	}
}
