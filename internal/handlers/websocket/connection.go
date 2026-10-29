package websockethandler

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/gorilla/websocket"
)

// dial connects to the host. It waits a moment first: the host is often
// started right before the client connects to it. It is a variable so a
// test can replace it — the connection is the one step that needs a host to
// talk to, and a test that cannot reach it cannot check what is done with
// the error it returns.
var dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
	time.Sleep(1 * time.Second)

	return dialerFor(tlsConfig).Dial(url, header)
}

// dialerFor returns the dialer used to reach the host. The TLS
// configuration is the one that checks the certificate of the host, so it
// has to reach the dialer: a dialer without it would use the system roots,
// and a self-signed certificate is in none of them.
func dialerFor(tlsConfig *tls.Config) *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	if tlsConfig != nil {
		dialer.TLSClientConfig = tlsConfig
	}

	return &dialer
}

// Connect dials the host and returns the live session.
//
// It is the transport step only: what the session carries and how the caller
// consumes it — sealed or not, terminal or GUI — belongs to the caller. The
// terminal loop lives in internal/chat, and the end-to-end wrapper above that.
func Connect(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (session.Session, error) {
	conn, url, err := openConnectionDial(address, port, token, useTLS, tlsConfig)
	if err != nil {
		return nil, err
	}

	fmt.Println("Connected to", url)

	return newConnSession(conn), nil
}

// openConnectionDial dials the host and returns the connection with the URL
// it reached. The errors that name a missing secret, or a host whose
// transport is not the one this client speaks, are produced here so every
// caller explains a refusal the same way.
func openConnectionDial(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (*websocket.Conn, string, error) {
	url := buildURL(address, port, useTLS)

	header := http.Header{}
	if token != "" {
		header.Set(auth.HeaderName, token)
	}

	conn, resp, err := dial(url, header, tlsConfig)
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return nil, url, fmt.Errorf("the host refused the connection: it needs the shared secret")
			case http.StatusBadRequest:
				return nil, url, fmt.Errorf("the host refused the connection: it is serving TLS (the default), and plaintext cannot reach it — start the host with --no-tls, or use a client that speaks TLS")
			}
		}

		// A TLS handshake that failed against a host which is not speaking
		// TLS is the one mismatch a user can fix with a flag, so it is worth
		// naming rather than leaving as a bare "first record does not look
		// like a TLS handshake".
		if useTLS && strings.Contains(err.Error(), "tls:") {
			return nil, url, fmt.Errorf("%w — the host may be serving plaintext: a v0.6.0 host does, and so does one started with --no-tls; retry with --no-tls", err)
		}

		return nil, url, err
	}

	return conn, url, nil
}

// buildURL returns the URL of the host. An address that carries a port is
// used as it is, so that the port is not appended twice.
func buildURL(address string, port int, useTLS bool) string {
	scheme := "ws"
	if useTLS {
		scheme = "wss"
	}

	if _, _, err := net.SplitHostPort(address); err == nil {
		return fmt.Sprintf("%s://%s/ws", scheme, address)
	}

	return fmt.Sprintf("%s://%s:%d/ws", scheme, address, port)
}
