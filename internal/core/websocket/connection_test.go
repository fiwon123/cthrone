package websocketcore

import (
	"crypto/tls"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// stubSession is a transport session that records what the wrapper sends. The
// seam under test only needs session.Session, so the test needs no socket.
type stubSession struct {
	*session.Base

	mu   sync.Mutex
	sent [][]byte
}

func newStub() *stubSession {
	return &stubSession{Base: session.NewBase("stub", "stub:8080")}
}

func (s *stubSession) Send(payload []byte) error {
	if s.IsClosed() {
		return session.ErrClosed
	}

	s.mu.Lock()
	s.sent = append(s.sent, append([]byte(nil), payload...))
	s.mu.Unlock()

	return nil
}

func (s *stubSession) Close(reason string) { s.NotifyClose(reason) }

func (s *stubSession) outgoing() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]byte(nil), s.sent...)
}

// The address of the host comes from the argument. There is no default for
// it — the command used to panic when the argument was missing, and a
// stack trace is not a usage message.
func TestConnectAddress(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no argument means no address",
			args: nil,
			want: "",
		},
		{
			name: "the argument is the address",
			args: []string{"192.168.1.5"},
			want: "192.168.1.5",
		},
		{
			name: "an address with a port is kept as it is",
			args: []string{"192.168.1.5:9000"},
			want: "192.168.1.5:9000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectAddress(tc.args); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// A missing address is an error the command can report, not a panic.
func TestConnectWithoutAnAddressFails(t *testing.T) {
	err := Connect(nil, app.New(8080), "", "", auth.TLS{})
	if err == nil {
		t.Fatal("expected the missing address to be an error")
	}

	if err.Error() != "the address of the host to connect to is required" {
		t.Errorf("expected the error to name what is missing, got %q", err.Error())
	}
}

// The error of the connection comes back out. It used to be dropped, so a
// host that was not running looked like a command that did nothing.
func TestConnectReturnsTheConnectionError(t *testing.T) {
	original := connect
	defer func() { connect = original }()

	connect = func(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (session.Session, error) {
		return nil, errors.New("no host in this test")
	}

	err := Connect([]string{"192.168.1.5"}, app.New(8080), "", "", auth.TLS{})
	if err == nil {
		t.Fatal("expected the connection to fail")
	}

	if err.Error() != "no host in this test" {
		t.Errorf("expected the connection error, got %q", err.Error())
	}
}

// The session a caller gets back is the sealed one. The whole point of doing
// this in the operation layer is that a caller cannot ask for the bare socket,
// so the type is the assertion.
func TestDialReturnsASealedSession(t *testing.T) {
	restore := stubConnection(t, newStub())
	defer restore()

	t.Setenv(auth.EnvToken, "")

	s, err := Dial([]string{"192.168.1.5"}, app.New(8080), strings.Repeat("s", 32), "", auth.TLS{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if _, ok := s.(*e2e.Conn); !ok {
		t.Fatalf("got %T, want the sealed session", s)
	}
}

// The first thing the transport carries is a handshake frame, not a word of
// the conversation: the session is sealed before it is handed over.
func TestDialSendsTheHandshakeFirst(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	t.Setenv(auth.EnvToken, "")

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), strings.Repeat("s", 32), "", auth.TLS{}); err != nil {
		t.Fatalf("dial: %v", err)
	}

	sent := stub.outgoing()
	if len(sent) == 0 {
		t.Fatal("the wrapper sent nothing")
	}

	frame, err := wire.Parse(sent[0])
	if err != nil {
		t.Fatalf("the first frame does not parse: %v", err)
	}

	if frame.Header.Kind != wire.KindHandshake {
		t.Errorf("the first frame is %q, want the handshake", frame.Header.Kind)
	}
}

// A secret too short to seal with is refused instead of used in the clear.
// This is the fail-closed rule: there is no plaintext fallback to fall back to.
func TestDialRefusesAShortSecret(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	t.Setenv(auth.EnvToken, "")

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), "short", "", auth.TLS{}); err == nil {
		t.Fatal("expected a short secret to be refused")
	}

	if !stub.IsClosed() {
		t.Error("expected the transport session to be closed, not left open")
	}
}

// Connect runs the terminal on the sealed session, not on the bare one.
func TestConnectRunsTheTerminalOnASealedSession(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	originalRun := runTerminal
	defer func() { runTerminal = originalRun }()

	var got session.Session

	runTerminal = func(s session.Session) { got = s }

	t.Setenv(auth.EnvToken, "")

	if err := Connect([]string{"192.168.1.5"}, app.New(8080), strings.Repeat("s", 32), "", auth.TLS{}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if _, ok := got.(*e2e.Conn); !ok {
		t.Fatalf("the terminal ran on %T, want the sealed session", got)
	}
}

// The secret comes from the same places it always did, so a connection that
// used to work with --token still does.
func TestDialUsesTheTokenFile(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	t.Setenv(auth.EnvToken, "")

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), "", "/nonexistent/cthrone-token", auth.TLS{}); err == nil {
		t.Fatal("expected an unreadable token file to be an error")
	}
}

// stubConnection replaces the dialler for one test and puts it back.
func stubConnection(t *testing.T, stub *stubSession) func() {
	t.Helper()

	original := connect

	connect = func(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (session.Session, error) {
		return stub, nil
	}

	return func() { connect = original }
}
