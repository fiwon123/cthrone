package websockethandler

import (
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/gorilla/websocket"
)

// fastDial removes the deliberate one-second wait Connect adds for a host
// that was started right before the client: a test starts the host first.
func fastDial(t *testing.T) {
	t.Helper()

	original := dial
	t.Cleanup(func() { dial = original })

	dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
		return dialerFor(tlsConfig).Dial(url, header)
	}
}

// A host and a client exchange messages through session.Session, with no
// terminal in sight. This is the path the GUI and the mobile app use.
func TestSessionExchangesMessages(t *testing.T) {
	fastDial(t)

	hostSessions := make(chan session.Session, 1)

	server, err := OpenServer(0, "127.0.0.1", "", "", "", func(s session.Session) {
		hostSessions <- s
	})
	if err != nil {
		t.Fatalf("OpenServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	host, portText, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", server.Addr(), err)
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	client, err := Dial(host, port, "", false, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close("test over") })

	var hostSession session.Session

	select {
	case hostSession = <-hostSessions:
	case <-time.After(2 * time.Second):
		t.Fatal("the host never handed out a session")
	}

	// Client -> host.
	fromClient := make(chan session.Message, 1)
	hostSession.OnMessage(func(m session.Message) { fromClient <- m })

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("client Send: %v", err)
	}

	select {
	case got := <-fromClient:
		if string(got.Payload) != "hello" {
			t.Errorf("expected %q, got %q", "hello", string(got.Payload))
		}

		// The peer's address travels beside the sender: the layer above
		// replaces the sender with the name the handshake authenticated, and
		// the address is what says which machine the name belongs to.
		if got.Addr == "" {
			t.Error("the host received no peer address")
		}

		if _, _, err := net.SplitHostPort(got.Addr); err != nil {
			t.Errorf("peer address %q is not host:port: %v", got.Addr, err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the host to receive the message")
	}

	// Host -> client.
	fromHost := make(chan string, 1)
	client.OnMessage(func(m session.Message) { fromHost <- string(m.Payload) })

	if err := hostSession.Send([]byte("hi")); err != nil {
		t.Fatalf("hostSession Send: %v", err)
	}

	select {
	case got := <-fromHost:
		if got != "hi" {
			t.Errorf("expected %q, got %q", "hi", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the client to receive the message")
	}
}
