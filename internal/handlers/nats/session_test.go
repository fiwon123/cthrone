package natshandler

import (
	"errors"
	"testing"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/nats-io/nats.go"
)

// A session publishes to the subject it subscribed to, and the callback the
// subscription carries feeds the session's OnMessage.
func TestSessionSendsAndReceives(t *testing.T) {
	pub := &fakePub{}
	sub := &fakeSub{}

	sess, err := newSession(pub, sub, func() {}, "chat")
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	if sub.subject != "chat" {
		t.Errorf("expected subscription on chat, got %q", sub.subject)
	}

	if sub.handler == nil {
		t.Fatal("expected the session to register a handler")
	}

	received := make(chan session.Message, 1)
	sess.OnMessage(func(m session.Message) { received <- m })

	sub.handler(&nats.Msg{Subject: "chat", Data: []byte("hi")})

	select {
	case got := <-received:
		if string(got.Payload) != "hi" {
			t.Errorf("expected %q, got %q", "hi", string(got.Payload))
		}
		if got.From != "chat" {
			t.Errorf("expected the message to name its subject, got %q", got.From)
		}
	default:
		t.Fatal("the session never delivered the message to OnMessage")
	}

	if err := sess.Send([]byte("yo")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(pub.published) != 1 || string(pub.published[0]) != "yo" {
		t.Errorf("expected the session to publish %q, got %q", "yo", pub.published)
	}

	if pub.subject != "chat" {
		t.Errorf("expected the message on chat, got %q", pub.subject)
	}
}

// A subscription that fails is reported, not swallowed: a session that cannot
// hear anything is not a working session.
func TestNewSessionReportsASubscribeError(t *testing.T) {
	boom := errors.New("no broker")

	_, err := newSession(&fakePub{}, &fakeSub{err: boom}, func() {}, "chat")
	if !errors.Is(err, boom) {
		t.Fatalf("expected the subscribe error, got %v", err)
	}
}

// A broker delivers a published message back to the publisher's own
// subscription. The session must not report that echo as an incoming message,
// or the sender sees its own line twice.
func TestSessionDropsItsOwnEcho(t *testing.T) {
	pub := &fakePub{}
	sub := &fakeSub{}

	sess, err := newSession(pub, sub, func() {}, "chat")
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	received := make(chan string, 2)
	sess.OnMessage(func(m session.Message) { received <- string(m.Payload) })

	if err := sess.Send([]byte("mine")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The broker echoing what was just published is not a new message.
	sub.handler(&nats.Msg{Subject: "chat", Data: []byte("mine")})

	select {
	case got := <-received:
		t.Fatalf("expected the echo to be dropped, got %q", got)
	default:
	}

	// A message from the peer is still delivered.
	sub.handler(&nats.Msg{Subject: "chat", Data: []byte("theirs")})

	select {
	case got := <-received:
		if got != "theirs" {
			t.Errorf("expected %q, got %q", "theirs", got)
		}
	default:
		t.Fatal("expected the peer message to be delivered")
	}
}

// Close unsubscribes, closes the connection and makes further sends fail.
func TestSessionClose(t *testing.T) {
	closed := false
	sess, err := newSession(&fakePub{}, &fakeSub{}, func() { closed = true }, "chat")
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	sess.Close("done")

	if !closed {
		t.Error("expected Close to close the connection")
	}

	if err := sess.Send([]byte("late")); err != session.ErrClosed {
		t.Errorf("expected session.ErrClosed, got %v", err)
	}
}
