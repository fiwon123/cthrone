package natshandler

import (
	"crypto/tls"
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

	_, err := Connect("nats://broker.internal:4222", "", "", nil)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if dialled != "nats://broker.internal:4222" {
		t.Errorf("expected the configured broker to be dialled, got %q", dialled)
	}
}

// The credentials reach the broker as options, not as part of the URL: a
// URL with a password in it is visible in the process list.
func TestConnectSendsTheCredentials(t *testing.T) {
	var options nats.Options

	original := dial
	defer func() { dial = original }()

	dial = func(url string, opts ...nats.Option) (*nats.Conn, error) {
		for _, option := range opts {
			option(&options)
		}

		return nil, errors.New("no broker in this test")
	}

	_, err := Connect("nats://broker.internal:4222", "cthrone", "a-secret", nil)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if options.User != "cthrone" {
		t.Errorf("expected the user to reach the broker, got %q", options.User)
	}

	if options.Password != "a-secret" {
		t.Errorf("expected the password to reach the broker, got %q", options.Password)
	}
}

// The TLS configuration reaches the broker: without it the client would
// use the system roots, and a broker with a self-signed certificate is in
// none of them.
func TestConnectSendsTheTLSConfiguration(t *testing.T) {
	var options nats.Options

	original := dial
	defer func() { dial = original }()

	dial = func(url string, opts ...nats.Option) (*nats.Conn, error) {
		for _, option := range opts {
			option(&options)
		}

		return nil, errors.New("no broker in this test")
	}

	config := &tls.Config{ServerName: "broker"}
	_, err := Connect("tls://broker.internal:4222", "", "", config)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if options.TLSConfig != config {
		t.Error("expected the TLS configuration to reach the broker")
	}
}

// A broker without credentials is not asked for any: a dev broker has no
// authorization, and refusing to talk to it would be wrong.
func TestConnectWithoutCredentialsAsksForNone(t *testing.T) {
	var options nats.Options

	original := dial
	defer func() { dial = original }()

	dial = func(url string, opts ...nats.Option) (*nats.Conn, error) {
		for _, option := range opts {
			option(&options)
		}

		return nil, errors.New("no broker in this test")
	}

	_, err := Connect("nats://broker.internal:4222", "", "", nil)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if options.User != "" || options.Password != "" {
		t.Errorf("expected no credentials, got %q/%q", options.User, options.Password)
	}
}
