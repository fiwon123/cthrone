package websockethandler

import (
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// An address that already carries a port is used as it is: appending the
// port flag to it makes a URL that is not one.
func TestBuildURL(t *testing.T) {
	cases := []struct {
		name    string
		address string
		port    int
		useTLS  bool
		want    string
	}{
		{
			name:    "an address without a port gets the port",
			address: "192.168.1.5",
			port:    8080,
			want:    "ws://192.168.1.5:8080/ws",
		},
		{
			name:    "an address with a port is used as it is",
			address: "192.168.1.5:9000",
			port:    8080,
			want:    "ws://192.168.1.5:9000/ws",
		},
		{
			name:    "a host name works too",
			address: "localhost",
			port:    8080,
			want:    "ws://localhost:8080/ws",
		},
		{
			name:    "the encrypted transport says so in the URL",
			address: "192.168.1.5",
			port:    8080,
			useTLS:  true,
			want:    "wss://192.168.1.5:8080/ws",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildURL(tc.address, tc.port, tc.useTLS); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// A connection that fails is reported. The error used to be dropped, so a
// host that was not running looked like a command that did nothing.
func TestConnectReturnsTheDialError(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("no host in this test")
	}

	_, err := Connect("192.168.1.5", 8080, "", false, nil)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if err.Error() != "no host in this test" {
		t.Errorf("expected the dial error, got %q", err.Error())
	}
}

// The client sends the shared secret. Without it a host that requires one
// refuses the connection.
func TestConnectSendsTheSharedSecret(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	var got string

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		got = header.Get("X-Cthrone-Token")

		return nil, nil, errors.New("stop here")
	}

	_, _ = Connect("192.168.1.5", 8080, "s3cret", false, nil)

	if got != "s3cret" {
		t.Errorf("expected the secret to be sent, got %q", got)
	}
}

// A host that refuses the connection says what is wrong, so the user knows
// the host needs a secret rather than guessing.
func TestConnectExplainsARefusedConnection(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		return nil, &http.Response{StatusCode: http.StatusUnauthorized}, errors.New("bad handshake")
	}

	_, err := Connect("192.168.1.5", 8080, "", false, nil)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	if err.Error() != "the host refused the connection: it needs the shared secret" {
		t.Errorf("expected the refusal to name the missing secret, got %q", err.Error())
	}
}

// A TLS handshake that fails because the host is serving plaintext is the one
// mismatch a flag can fix — a v0.6.0 host, or one started with --no-tls — so
// the error names it instead of leaving the handshake error bare.
func TestConnectExplainsAPlaintextHost(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("tls: first record does not look like a TLS handshake")
	}

	_, err := Connect("192.168.1.5", 8080, "", true, &tls.Config{})
	if err == nil {
		t.Fatal("expected the connection to fail")
	}

	if !strings.Contains(err.Error(), "--no-tls") {
		t.Errorf("expected the error to name the way out, got %q", err.Error())
	}
}
