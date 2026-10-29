package session

import (
	"testing"
	"time"
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
