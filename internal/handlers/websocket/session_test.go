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

	server, err := OpenServer(0, "127.0.0.1", "", true, "", "", func(s session.Session) {
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
	fromClient := make(chan string, 1)
	hostSession.OnMessage(func(m session.Message) { fromClient <- string(m.Payload) })

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("client Send: %v", err)
	}

	select {
	case got := <-fromClient:
		if got != "hello" {
			t.Errorf("expected %q, got %q", "hello", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the host session never received the message")
	}

	// Host -> client.
	fromHost := make(chan string, 1)
	client.OnMessage(func(m session.Message) { fromHost <- string(m.Payload) })

	if err := hostSession.Send([]byte("hi back")); err != nil {
		t.Fatalf("host Send: %v", err)
	}

	select {
	case got := <-fromHost:
		if got != "hi back" {
			t.Errorf("expected %q, got %q", "hi back", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the client never received the reply")
	}
}

// A host that refuses the secret does not hand out a session, and the client
// that dialed it gets the refusal that names the missing secret.
func TestDialAgainstAHostThatNeedsTheSecret(t *testing.T) {
	fastDial(t)

	server, err := OpenServer(0, "127.0.0.1", "s3cret", false, "", "", nil)
	if err != nil {
		t.Fatalf("OpenServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	host, portText, _ := net.SplitHostPort(server.Addr())
	port, _ := strconv.Atoi(portText)

	_, err = Dial(host, port, "", false, nil)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	if err.Error() != "the host refused the connection: it needs the shared secret" {
		t.Errorf("expected the refusal to name the missing secret, got %q", err.Error())
	}
}

// After a session is closed, Send reports it rather than writing to a dead
// connection.
func TestSendAfterCloseIsRefused(t *testing.T) {
	fastDial(t)

	server, err := OpenServer(0, "127.0.0.1", "", true, "", "", nil)
	if err != nil {
		t.Fatalf("OpenServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	host, portText, _ := net.SplitHostPort(server.Addr())
	port, _ := strconv.Atoi(portText)

	client, err := Dial(host, port, "", false, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	client.Close("done")

	if err := client.Send([]byte("late")); err != session.ErrClosed {
		t.Errorf("expected session.ErrClosed, got %v", err)
	}
}

// Stop closes the listener: the address it reported no longer accepts
// connections.
func TestStopClosesTheListener(t *testing.T) {
	server, err := OpenServer(0, "127.0.0.1", "", true, "", "", nil)
	if err != nil {
		t.Fatalf("OpenServer: %v", err)
	}

	addr := server.Addr()
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		t.Error("expected the port to be closed after Stop")
	}
}
