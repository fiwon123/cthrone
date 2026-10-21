package websockethandler

import (
	"crypto/tls"
	"errors"
	"net/http"
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

	err := Connect("192.168.1.5", 8080, "", false, nil)
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

	_ = Connect("192.168.1.5", 8080, "s3cret", false, nil)

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

	err := Connect("192.168.1.5", 8080, "", false, nil)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	if err.Error() != "the host refused the connection: it needs the shared secret" {
		t.Errorf("expected the refusal to name the missing secret, got %q", err.Error())
	}
}

// The client hands the TLS configuration to the dialer: the certificate is
// checked against the fingerprint, and the dialer is the one that does it.
func TestConnectPassesTheTLSConfiguration(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	var got *tls.Config

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		got = tlsConfig

		return nil, nil, errors.New("stop here")
	}

	config := &tls.Config{ServerName: "cthrone"}

	_ = Connect("192.168.1.5", 8080, "", true, config)

	if got != config {
		t.Error("expected the TLS configuration to reach the dialer")
	}
}

// The TLS configuration reaches the dialer. A dialer without it would use
// the system roots, and a self-signed certificate is in none of them.
func TestDialerUsesTheTLSConfiguration(t *testing.T) {
	config := &tls.Config{ServerName: "cthrone"}

	dialer := dialerFor(config)
	if dialer.TLSClientConfig != config {
		t.Error("expected the TLS configuration to reach the dialer")
	}

	plain := dialerFor(nil)
	if plain.TLSClientConfig != nil {
		t.Error("expected no TLS configuration without TLS")
	}
}

// A client that talks plain websocket to a host that serves TLS is told
// what is wrong: "bad handshake" on its own sends the user looking in the
// wrong place.
func TestConnectExplainsAPlainClientAgainstATLSHost(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		return nil, &http.Response{StatusCode: http.StatusBadRequest}, errors.New("bad handshake")
	}

	err := Connect("192.168.1.5", 8080, "", false, nil)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	if err.Error() != "the host refused the connection: it is serving TLS, add --tls" {
		t.Errorf("expected the error to name the missing --tls, got %q", err.Error())
	}
}
