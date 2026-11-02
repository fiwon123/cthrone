//go:build gui

package ui

import (
	"testing"

	"github.com/fiwon123/cthrone/internal/wire"
)

// A message a host relays is drawn under the name that typed it, not under the
// host's, and only once (#431).
func TestARelayedMessageIsAttributedToItsSender(t *testing.T) {
	g := newScreenGUI(t, false)

	frame, err := wire.Encode(wire.Header{Kind: wire.KindText, Sender: "alice (10.0.0.9)"}, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	g.receiveFrame("host", frame)

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.messages) != 1 {
		t.Fatalf("%d messages, want 1", len(g.messages))
	}

	row := g.messages[0]
	if row.from != "alice (10.0.0.9)" {
		t.Errorf("the message is from %q, want the sender in the frame", row.from)
	}

	if row.text != "hello" {
		t.Errorf("the message says %q, want the sentence alone", row.text)
	}
}

// A message that was not relayed keeps the transport's name.
func TestADirectMessageKeepsTheTransportName(t *testing.T) {
	g := newScreenGUI(t, false)

	g.receiveFrame("laptop (10.0.0.7)", []byte("hello"))

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.messages) != 1 || g.messages[0].from != "laptop (10.0.0.7)" {
		t.Fatalf("messages = %+v, want the peer's label", g.messages)
	}
}
