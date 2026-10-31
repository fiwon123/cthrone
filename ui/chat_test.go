package ui

import (
	"fmt"
	"strings"
	"testing"
)

// TestChatMessageLine checks how a message reaches the screen and the "Copy all"
// clipboard: the same rendering for both, so a copy carries what was read.
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
