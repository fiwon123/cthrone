package ui

import (
	"fmt"
	"strings"
	"testing"
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

// transcript is the whole-log rendering the tests assert: order kept, one line
// per message. It is the production transcriptOf, wrapped so the log-cap and
// line tests read like the clipboard action they check (#469).
func transcript(messages []chatMessage) string {
	return transcriptOf(messages)
}

// TestSelectionRange covers the two taps a reader makes: either end may be the
// first one, and the span is the same either way (#469).
func TestSelectionRange(t *testing.T) {
	tests := []struct {
		name         string
		sel          selection
		wantStart    int
		wantEnd      int
		wantCount    int
		wantContains []int
		wantExcluded []int
	}{
		{
			name:      "nothing tapped yet",
			sel:       selection{active: true, anchor: noSelection, focus: noSelection},
			wantStart: noSelection,
			wantEnd:   noSelection,
		},
		{
			name:      "one message",
			sel:       selection{active: true, anchor: 2, focus: 2},
			wantStart: 2,
			wantEnd:   2,
			wantCount: 1,
			// 2 is both the start and the end; 1 and 3 are outside.
			wantContains: []int{2},
			wantExcluded: []int{0, 1, 3},
		},
		{
			name:         "downwards",
			sel:          selection{active: true, anchor: 1, focus: 3},
			wantStart:    1,
			wantEnd:      3,
			wantCount:    3,
			wantContains: []int{1, 2, 3},
			wantExcluded: []int{0, 4},
		},
		{
			name:         "upwards",
			sel:          selection{active: true, anchor: 3, focus: 1},
			wantStart:    1,
			wantEnd:      3,
			wantCount:    3,
			wantContains: []int{1, 2, 3},
			wantExcluded: []int{0, 4},
		},
		{
			name:      "inactive covers nothing",
			sel:       selection{active: false, anchor: 1, focus: 3},
			wantStart: 1,
			wantEnd:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sel.start(); got != tt.wantStart {
				t.Errorf("start() = %d, want %d", got, tt.wantStart)
			}

			if got := tt.sel.end(); got != tt.wantEnd {
				t.Errorf("end() = %d, want %d", got, tt.wantEnd)
			}

			if got := tt.sel.count(); got != tt.wantCount {
				t.Errorf("count() = %d, want %d", got, tt.wantCount)
			}

			for _, index := range tt.wantContains {
				if !tt.sel.contains(index) {
					t.Errorf("contains(%d) = false, want true", index)
				}
			}

			for _, index := range tt.wantExcluded {
				if tt.sel.contains(index) {
					t.Errorf("contains(%d) = true, want false", index)
				}
			}
		})
	}
}

// TestRemoveRange cuts a span out of the log: the head, the tail and the middle
// keep what is left in order (#469).
func TestRemoveRange(t *testing.T) {
	messages := []chatMessage{{text: "a"}, {text: "b"}, {text: "c"}, {text: "d"}}

	tests := []struct {
		name string
		from int
		to   int
		want []string
	}{
		{"the middle two", 1, 2, []string{"a", "d"}},
		{"the head", 0, 1, []string{"c", "d"}},
		{"the tail", 2, 3, []string{"a", "b"}},
		{"one message", 2, 2, []string{"a", "b", "d"}},
		{"everything", 0, len(messages) - 1, nil},
		{"past the end", 2, 99, []string{"a", "b"}},
		{"an empty span", 2, 1, []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeRange(append([]chatMessage(nil), messages...), tt.from, tt.to)

			if len(got) != len(tt.want) {
				t.Fatalf("removeRange(%d, %d) left %d message(s), want %d", tt.from, tt.to, len(got), len(tt.want))
			}

			for i, want := range tt.want {
				if got[i].text != want {
					t.Errorf("message %d = %q, want %q", i, got[i].text, want)
				}
			}
		})
	}
}

// TestHiddenBodyAndLine checks what a hidden row draws and what a copy takes:
// the mask on screen, the secret on the clipboard (#469).
func TestHiddenBodyAndLine(t *testing.T) {
	hidden := chatMessage{from: "laptop", text: "hunter2", hidden: true}

	if got := hidden.body(); got != hiddenBody {
		t.Errorf("body() = %q, want the mask", got)
	}

	if got := hidden.line(); got != "laptop: hunter2" {
		t.Errorf("line() = %q, want the text a copy should keep", got)
	}

	revealed := hidden
	revealed.revealed = true

	if got := revealed.body(); got != "hunter2" {
		t.Errorf("revealed body() = %q, want the text", got)
	}
}
