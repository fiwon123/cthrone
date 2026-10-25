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
