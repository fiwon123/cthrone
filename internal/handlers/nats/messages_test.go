package natshandler

import (
	"bytes"
	"errors"
	"log"
	"testing"

	"github.com/nats-io/nats.go"
)

type fakePub struct {
	published  [][]byte
	subject    string
	flushErr   error
	publishErr error
	flushed    bool
}

func (f *fakePub) Publish(subj string, data []byte) error {
	f.subject = subj
	f.published = append(f.published, append([]byte(nil), data...))
	return f.publishErr
}

func (f *fakePub) Flush() error {
	f.flushed = true
	return f.flushErr
}

func TestPublishMessages(t *testing.T) {
	f := &fakePub{}
	err := publishMessages(f, "hello", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if f.subject != "chat" {
		t.Errorf("expected subject chat, got %s", f.subject)
	}
	if string(f.published[0]) != "hello" {
		t.Errorf("expected hello, got %s", string(f.published[0]))
	}
	if !f.flushed {
		t.Error("expected flush to be called")
	}
}

func TestPublishMessagesError(t *testing.T) {
	f := &fakePub{publishErr: errors.New("boom")}
	err := publishMessages(f, "hello", "chat")
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeSub struct {
	subject string
	handler nats.MsgHandler
	err     error
}

type fakeSubscription struct{}

func (f *fakeSubscription) Unsubscribe() error { return nil }

func (f *fakeSub) Subscribe(subj string, h nats.MsgHandler) (subscription, error) {
	f.subject = subj
	f.handler = h
	return &fakeSubscription{}, f.err
}

func TestSubscribeMessages(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	f := &fakeSub{}
	subscribeMessages(f, "chat")
	if f.subject != "chat" {
		t.Errorf("expected chat, got %s", f.subject)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Subscriptions active")) {
		t.Errorf("expected subscriptions active log, got %s", buf.String())
	}
	if f.handler == nil {
		t.Fatal("expected handler registered")
	}
	f.handler(&nats.Msg{Data: []byte("hi")})
	if !bytes.Contains(buf.Bytes(), []byte("hi")) {
		t.Errorf("expected message logged, got %s", buf.String())
	}
}
