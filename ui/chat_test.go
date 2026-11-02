package ui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2/widget"
)

// TestChatMessageLine checks how a message reaches the screen: the row and the
// clipboard carry the same rendering, so a copy is what was read.
func TestChatMessageLine(t *testing.T) {
	tests := []struct {
		name string
		msg  chatMessage
		want string
	}{
		{"own message", chatMessage{text: "hello"}, "You: hello"},
		{"own empty text", chatMessage{}, "You: "},
		{"peer message", chatMessage{from: "laptop", text: "hi"}, "laptop: hi"},
		{"peer with empty text", chatMessage{from: "laptop"}, "laptop: "},
		{"colon in the text", chatMessage{from: "laptop", text: "a: b"}, "laptop: a: b"},
		{"multi-line text", chatMessage{text: "one\ntwo"}, "You: one\ntwo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.line(); got != tt.want {
				t.Errorf("line() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSenderLabel checks how an arriving message is attributed: the name the
// handshake authenticated, with the address beside it when the transport has
// one — the same rule the terminal prints by.
func TestSenderLabel(t *testing.T) {
	tests := []struct {
		name string
		from string
		addr string
		want string
	}{
		{"a bus has no address", "laptop", "", "laptop"},
		{"name and address", "laptop", "192.168.1.20:51234", "laptop (192.168.1.20:51234)"},
		{"an address that is the name", "laptop", "laptop", "laptop"},
		{"an unnamed peer with an address", "", "192.168.1.20:51234", "peer (192.168.1.20:51234)"},
		{"nothing at all", "", "", "peer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := senderLabel(tt.from, tt.addr); got != tt.want {
				t.Errorf("senderLabel(%q, %q) = %q, want %q", tt.from, tt.addr, got, tt.want)
			}
		})
	}
}

// TestTranscript checks the whole-log copy: order kept, one line per message,
// and nothing for an empty log.
func TestTranscript(t *testing.T) {
	tests := []struct {
		name     string
		messages []chatMessage
		want     string
	}{
		{"empty", nil, ""},
		{"one", []chatMessage{{text: "hello"}}, "You: hello"},
		{
			"in order",
			[]chatMessage{{text: "hi"}, {from: "laptop", text: "hello"}},
			"You: hi\nlaptop: hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transcript(tt.messages); got != tt.want {
				t.Errorf("transcript() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAppendMessageKeepsTheCap checks the two things the view relies on: the
// log stops growing at maxMessages, and what survives is the newest messages in
// order.
func TestAppendMessageKeepsTheCap(t *testing.T) {
	var messages []chatMessage

	for i := 0; i < maxMessages; i++ {
		messages = appendMessage(messages, chatMessage{text: fmt.Sprintf("m%d", i)})
	}

	if len(messages) != maxMessages {
		t.Fatalf("len(messages) = %d, want %d", len(messages), maxMessages)
	}

	messages = appendMessage(messages, chatMessage{text: "newest"})
	if len(messages) != maxMessages {
		t.Fatalf("len(messages) = %d after the drop, want %d", len(messages), maxMessages)
	}

	// The oldest message is gone, the newest is last, and the rest keep order.
	if first := messages[0].text; first != "m1" {
		t.Errorf("oldest kept message = %q, want %q", first, "m1")
	}

	if last := messages[len(messages)-1].text; last != "newest" {
		t.Errorf("newest message = %q, want %q", last, "newest")
	}

	// The buffer is reused, not walked forward: a long-lived window must not
	// allocate a new backing array on every line once it is full.
	before := cap(messages)
	for i := 0; i < maxMessages*2; i++ {
		messages = appendMessage(messages, chatMessage{text: "more"})
	}

	if grew := cap(messages) > before; grew {
		t.Errorf("cap(messages) grew from %d to %d while the log was full", before, cap(messages))
	}

	if len(messages) != maxMessages {
		t.Errorf("len(messages) = %d after churn, want %d", len(messages), maxMessages)
	}

	if joined := transcript(messages); strings.Count(joined, "\n") != maxMessages-1 {
		t.Errorf("transcript has %d lines, want %d", strings.Count(joined, "\n")+1, maxMessages)
	}
}

// TestDisconnectLine checks the transcript line a close leaves behind: it names
// the peer that left and keeps the reason when there is one.
func TestDisconnectLine(t *testing.T) {
	tests := []struct {
		name   string
		peer   string
		reason string
		want   string
	}{
		{"named", "laptop", "peer said goodbye", "laptop disconnected: peer said goodbye"},
		{"unnamed", "", "EOF", "peer disconnected: EOF"},
		{"no reason", "laptop", "", "laptop disconnected"},
		{"nothing", "", "", "peer disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := disconnectLine(tt.peer, tt.reason); got != tt.want {
				t.Errorf("disconnectLine(%q, %q) = %q, want %q", tt.peer, tt.reason, got, tt.want)
			}
		})
	}
}

// TestPeerNamePrefersTheHandshake checks the name a close reports: the handshake
// is authoritative, a sender fills the gap, and an unnamed session still has a
// word to print.
func TestPeerNamePrefersTheHandshake(t *testing.T) {
	t.Run("handshake wins", func(t *testing.T) {
		p := &peerName{}

		p.learn("192.168.1.5:5555")
		p.set("laptop")
		p.learn("192.168.1.5:5555")

		if got := p.get(); got != "laptop" {
			t.Errorf("peer name = %q, want %q", got, "laptop")
		}
	})

	t.Run("sender fills the gap", func(t *testing.T) {
		p := &peerName{}
		p.learn("192.168.1.5:5555")

		if got := p.get(); got != "192.168.1.5:5555" {
			t.Errorf("peer name = %q, want the sender", got)
		}
	})

	t.Run("nothing names it", func(t *testing.T) {
		if got := (&peerName{}).get(); got != "peer" {
			t.Errorf("peer name = %q, want %q", got, "peer")
		}
	})
}

// TestComposerHint checks what the message field promises: Enter starts a new
// line on both platforms, and the hint names the action that sends — a key
// combination on a desktop, the button on a phone, which has no Shift+Enter.
func TestComposerHint(t *testing.T) {
	tests := []struct {
		name    string
		stack   bool
		want    string
		mustHas string
	}{
		{"desktop", false, "Type a message… Enter starts a new line, Shift+Enter sends it", "Shift+Enter"},
		{"touch", true, "Type a message… Enter starts a new line, Send sends it", "Send"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := composerHint(tt.stack)
			if got != tt.want {
				t.Errorf("composerHint(%v) = %q, want %q", tt.stack, got, tt.want)
			}

			// The hint has to keep saying what Enter does: a user who expects
			// it to send reads a field that looks broken otherwise.
			if !strings.Contains(got, "new line") {
				t.Errorf("composerHint(%v) = %q, which does not say what Enter does", tt.stack, got)
			}

			if !strings.Contains(got, tt.mustHas) {
				t.Errorf("composerHint(%v) = %q, which does not name %q", tt.stack, got, tt.mustHas)
			}
		})
	}
}

// The composer starts at one line and grows with the message, up to a ceiling
// where the text scrolls inside it (#324). Past that it stops being a composer
// and starts being the screen.
func TestComposerRows(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		columns int
		want    int
	}{
		{"empty", "", 40, 1},
		{"one line", "hello", 40, 1},
		{"a newline", "hello\n", 40, 2},
		{"three lines", "a\nb\nc", 40, 3},
		{"a long paragraph wraps", strings.Repeat("x", 120), 40, 3},
		{"more than the ceiling", strings.Repeat("x\n", 20), 40, composerRowsMax},
		{"no measurement yet uses the default", strings.Repeat("x", 30), 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composerRows(tt.text, tt.columns); got != tt.want {
				t.Errorf("composerRows(%q, %d) = %d, want %d", tt.text, tt.columns, got, tt.want)
			}
		})
	}
}

// transcript renders every message in order, one per line.
//
// It used to be the app's Copy all action as well; that button is gone (#289),
// and what is left is the test helper the log-cap and line tests need.
func transcript(messages []chatMessage) string {
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.line()
	}

	return strings.Join(lines, "\n")
}

// The composer's actions read like a chat app's: a paperclip, and Send as the
// primary action (#324). This is the shape, not the pixels.
func TestComposerActionsReadLikeAChatApp(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			if got := g.attachBtn.Text(); got != "" {
				t.Errorf("the attach action shows %q, want the paperclip alone", got)
			}

			if got := g.sendBtn.Importance(); got != widget.HighImportance {
				t.Errorf("Send has importance %v, want the primary one", got)
			}
		})
	}
}
