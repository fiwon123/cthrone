package session

import (
	"errors"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/wire"
)

type fakeSession struct {
	*Base
	sent [][]byte
}

func newFake(id, remote string) *fakeSession {
	return &fakeSession{Base: NewBase(id, remote)}
}

func (f *fakeSession) Send(payload []byte) error {
	if f.IsClosed() {
		return ErrClosed
	}
	cp := append([]byte(nil), payload...)
	f.sent = append(f.sent, cp)
	return nil
}

func (f *fakeSession) Close(reason string) {
	f.NotifyClose(reason)
}

func TestHubBroadcast(t *testing.T) {
	h := NewHub()
	a := newFake("a", "1.1.1.1")
	b := newFake("b", "2.2.2.2")
	c := newFake("c", "3.3.3.3")
	h.Register(a)
	h.Register(b)
	h.Register(c)

	h.Broadcast("a", []byte("hello"))

	if len(b.sent) != 1 || string(b.sent[0]) != "hello" {
		t.Fatalf("b should receive message")
	}
	if len(c.sent) != 1 {
		t.Fatalf("c should receive message")
	}
	if len(a.sent) != 0 {
		t.Fatalf("sender should not receive own message")
	}
}

func TestBaseNotify(t *testing.T) {
	b := NewBase("x", "r")
	var got string
	b.OnMessage(func(m Message) { got = string(m.Payload) })
	b.NotifyMessage(Message{Payload: []byte("hi"), Time: time.Now()})
	if got != "hi" {
		t.Fatalf("want hi got %s", got)
	}

	var reason string
	b.OnClose(func(r string) { reason = r })
	b.NotifyClose("bye")
	if reason != "bye" {
		t.Fatalf("want bye got %s", reason)
	}
}

// A frame that arrives before a handler is installed must reach it, in the
// order it arrived: a transport starts reading the moment it is open, and the
// handshake hello is a frame the layer above has not hooked its callback for
// yet.
func TestBaseHoldsMessagesUntilOnMessage(t *testing.T) {
	b := NewBase("x", "r")

	b.NotifyMessage(Message{Payload: []byte("first"), Time: time.Now()})
	b.NotifyMessage(Message{Payload: []byte("second"), Time: time.Now()})

	var got []string

	b.OnMessage(func(m Message) { got = append(got, string(m.Payload)) })

	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("held messages = %v, want [first second]", got)
	}

	b.NotifyMessage(Message{Payload: []byte("third"), Time: time.Now()})

	if got[len(got)-1] != "third" {
		t.Fatalf("a live message was not delivered after the held ones: %v", got)
	}
}

// A handler that re-enters NotifyMessage must not recurse or reorder: the
// re-entrant frame is queued and delivered after the current one returns.
func TestBaseHandlesReentrantNotify(t *testing.T) {
	b := NewBase("x", "r")

	var got []string

	b.OnMessage(func(m Message) {
		got = append(got, string(m.Payload))

		if string(m.Payload) == "one" {
			b.NotifyMessage(Message{Payload: []byte("two"), Time: time.Now()})
		}
	})

	b.NotifyMessage(Message{Payload: []byte("one"), Time: time.Now()})

	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("re-entrant delivery = %v, want [one two]", got)
	}
}

// The buffer that holds frames arriving before a handler exists has a ceiling:
// the peer decides what it sends in that window, so it must not be able to make
// the process grow without one. What is held is the oldest messages, in order.
func TestBaseCapsWhatItHolds(t *testing.T) {
	b := NewBase("x", "r")

	for i := 0; i < maxPending+50; i++ {
		b.NotifyMessage(Message{Payload: []byte{byte(i)}, Time: time.Now()})
	}

	var got []Message

	b.OnMessage(func(m Message) { got = append(got, m) })

	if len(got) != maxPending {
		t.Fatalf("delivered %d held messages, want the cap of %d", len(got), maxPending)
	}

	for i, m := range got {
		if m.Payload[0] != byte(i) {
			t.Fatalf("held message %d carries %d, want them in arrival order", i, m.Payload[0])
		}
	}
}

// brokenSession fails every send, the way a peer whose socket died does.
type brokenSession struct{ *Base }

func (b *brokenSession) Send([]byte) error { return errors.New("socket gone") }
func (b *brokenSession) Close(string)      {}

// A room is a fan-out: a message from one device reaches every other device,
// and never comes back to the one that sent it.
func TestHubRelaysToEveryOtherSession(t *testing.T) {
	h := NewHub()

	a, b, c := newFake("a", "1.1.1.1"), newFake("b", "2.2.2.2"), newFake("c", "3.3.3.3")
	h.Register(a)
	h.Register(b)
	h.Register(c)

	a.NotifyMessage(Message{From: "alice", Addr: "1.1.1.1", Payload: []byte("hello")})

	want := "alice (1.1.1.1): hello"

	for _, s := range []*fakeSession{b, c} {
		if len(s.sent) != 1 || string(s.sent[0]) != want {
			t.Fatalf("%s received %q, want %q", s.ID(), s.sent, want)
		}
	}

	if len(a.sent) != 0 {
		t.Fatalf("the sender received its own message back: %q", a.sent)
	}
}

// A relayed file carries the sender in its header, because a file has no line
// to prefix. The chunk itself must arrive unchanged.
func TestRelayedPayloadNamesTheSenderOfAFile(t *testing.T) {
	frame, err := wire.Encode(wire.Header{
		Kind:   wire.KindFile,
		ID:     "f1",
		Name:   "notes.txt",
		Size:   3,
		Chunks: 1,
	}, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}

	relayed := RelayedPayload(Message{From: "alice", Addr: "192.168.0.5", Payload: frame})

	parsed, err := wire.Parse(relayed)
	if err != nil {
		t.Fatalf("the relayed frame does not parse: %v", err)
	}

	if parsed.Header.Sender != "alice (192.168.0.5)" {
		t.Errorf("sender = %q, want %q", parsed.Header.Sender, "alice (192.168.0.5)")
	}

	if string(parsed.Payload) != "abc" {
		t.Errorf("chunk = %q, want abc", parsed.Payload)
	}
}

// TestRelayedPayloadUnwrapsATextFrame covers the sentence a sender had to wrap
// because it begins with the magic: the label rule is the same for it as for
// any other line, and an older peer that knows nothing of the wrapper reads a
// sentence rather than a frame.
func TestRelayedPayloadUnwrapsATextFrame(t *testing.T) {
	frame, err := wire.Encode(wire.Header{Kind: wire.KindText}, []byte("CTH1 says hello"))
	if err != nil {
		t.Fatal(err)
	}

	relayed := RelayedPayload(Message{From: "alice", Addr: "192.168.0.5", Payload: frame})

	if got := string(relayed); got != "alice (192.168.0.5): CTH1 says hello" {
		t.Errorf("RelayedPayload = %q, want the labelled line", got)
	}

	if wire.IsFrame(relayed) {
		t.Errorf("the relayed line is still a frame: %q", relayed)
	}
}

func TestRelayedPayloadPrefixesText(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{"name and address", Message{From: "alice", Addr: "192.168.0.5", Payload: []byte("hello")}, "alice (192.168.0.5): hello"},
		{"name only", Message{From: "alice", Payload: []byte("hello")}, "alice: hello"},
		{"unnamed peer", Message{Payload: []byte("hello")}, "peer: hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(RelayedPayload(tt.msg)); got != tt.want {
				t.Errorf("RelayedPayload = %q, want %q", got, tt.want)
			}
		})
	}
}

// One send failing must not stop the message reaching the rest: a device whose
// socket died is not the room's problem.
func TestBroadcastKeepsGoingWhenOneSendFails(t *testing.T) {
	h := NewHub()

	dead := &brokenSession{Base: NewBase("dead", "0.0.0.0")}
	live := newFake("live", "1.1.1.1")

	h.Register(dead)
	h.Register(live)

	if sent := h.Broadcast("", []byte("hello")); sent != 1 {
		t.Fatalf("Broadcast delivered to %d sessions, want 1", sent)
	}

	if len(live.sent) != 1 {
		t.Fatalf("the live session received %q, want one message", live.sent)
	}
}

// A session that is already closed is skipped rather than written to.
func TestBroadcastSkipsAClosedSession(t *testing.T) {
	h := NewHub()

	closed := newFake("closed", "0.0.0.0")
	closed.NotifyClose("gone")

	h.Register(closed)

	if sent := h.Broadcast("", []byte("hello")); sent != 0 {
		t.Fatalf("Broadcast delivered to %d sessions, want none", sent)
	}

	if len(closed.sent) != 0 {
		t.Fatalf("a closed session was written to: %q", closed.sent)
	}
}

// A device that leaves is taken out of the room, so the next message does not
// go to it.
func TestHubUnregistersOnClose(t *testing.T) {
	h := NewHub()

	a, b := newFake("a", "1.1.1.1"), newFake("b", "2.2.2.2")
	h.Register(a)
	h.Register(b)

	b.Close("peer said goodbye")

	if got := h.Count(); got != 1 {
		t.Fatalf("the room holds %d sessions after a departure, want 1", got)
	}

	h.Broadcast("", []byte("hello"))

	if len(b.sent) != 0 {
		t.Fatalf("a departed session received a message: %q", b.sent)
	}

	if len(a.sent) != 1 {
		t.Fatalf("the remaining session received %q, want one message", a.sent)
	}
}

// The consumer and an added handler both see every message: a relay must not
// take the printer's place, and the printer must not take the relay's.
func TestAddMessageHandlerRunsBesideTheConsumer(t *testing.T) {
	b := NewBase("x", "r")

	var consumer, extra []string

	b.OnMessage(func(m Message) { consumer = append(consumer, string(m.Payload)) })
	b.AddMessageHandler(func(m Message) { extra = append(extra, string(m.Payload)) })

	b.NotifyMessage(Message{Payload: []byte("one")})

	if len(consumer) != 1 || consumer[0] != "one" {
		t.Fatalf("consumer saw %v, want [one]", consumer)
	}

	if len(extra) != 1 || extra[0] != "one" {
		t.Fatalf("added handler saw %v, want [one]", extra)
	}
}

// A handler added after frames arrived still gets them: the relay is installed
// on a session that may already have delivered a frame.
func TestAddMessageHandlerReplaysHeldMessages(t *testing.T) {
	b := NewBase("x", "r")

	b.NotifyMessage(Message{Payload: []byte("first")})
	b.NotifyMessage(Message{Payload: []byte("second")})

	var got []string

	b.AddMessageHandler(func(m Message) { got = append(got, string(m.Payload)) })

	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("added handler saw %v, want [first second]", got)
	}
}

func TestAddCloseHandlerRunsBesideTheConsumer(t *testing.T) {
	b := NewBase("x", "r")

	var consumer, extra string

	b.OnClose(func(reason string) { consumer = reason })
	b.AddCloseHandler(func(reason string) { extra = reason })

	b.NotifyClose("bye")

	if consumer != "bye" || extra != "bye" {
		t.Fatalf("close handlers saw consumer=%q extra=%q, want both bye", consumer, extra)
	}
}

// A caller that installs its hooks after the listener bound must still learn
// about a device that joined in between.
func TestOnJoinReplaysTheSessionsAlreadyInTheRoom(t *testing.T) {
	h := NewHub()

	a := newFake("a", "1.1.1.1")
	h.Register(a)

	var joined []string

	h.OnJoin(func(s Session) { joined = append(joined, s.ID()) })

	if len(joined) != 1 || joined[0] != "a" {
		t.Fatalf("OnJoin replayed %v, want [a]", joined)
	}
}
