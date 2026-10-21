package natshandler

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
)

// The broker the client connects to is the configured one. This is the last
// step of the chain — the flag, the core and the handler all carry the URL,
// and here it reaches the function that dials the broker.
func TestConnectDialsTheConfiguredBroker(t *testing.T) {
	var dialled string

	original := dial
	defer func() { dial = original }()

	dial = func(url string, options ...nats.Option) (*nats.Conn, error) {
		dialled = url
		return nil, errors.New("no broker in this test")
	}

	_, err := Connect("nats://broker.internal:4222")
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if dialled != "nats://broker.internal:4222" {
		t.Errorf("expected the configured broker to be dialled, got %q", dialled)
	}
}
