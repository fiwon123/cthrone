package websocketcore

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/pairing"
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
	err := Connect(nil, app.New(8080), pairing.Options{}, auth.TLS{})
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

	// The connection fails before a session exists, but the credential is
	// resolved first; no test may read or create the real user's identity.
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := Connect([]string{"192.168.1.5"}, app.New(8080), pairing.Options{}, auth.TLS{})
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

	s, err := Dial([]string{"192.168.1.5"}, app.New(8080), pairing.Options{FlagToken: strings.Repeat("s", 32)}, auth.TLS{})
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

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), pairing.Options{FlagToken: strings.Repeat("s", 32)}, auth.TLS{}); err != nil {
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
// This is the fail-closed rule: there is no plaintext fallback to fall back to,
// and the refusal happens before the socket is dialled.
func TestDialRefusesAShortSecret(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	t.Setenv(auth.EnvToken, "")

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), pairing.Options{FlagToken: "short"}, auth.TLS{}); !errors.Is(err, e2e.ErrSecret) {
		t.Fatalf("Dial with a short secret = %v, want ErrSecret", err)
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

	if err := Connect([]string{"192.168.1.5"}, app.New(8080), pairing.Options{FlagToken: strings.Repeat("s", 32)}, auth.TLS{}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if _, ok := got.(*e2e.Conn); !ok {
		t.Fatalf("the terminal ran on %T, want the sealed session", got)
	}
}

// The secret comes from the same places it always did, so a connection that
// used to work with a token file still does.
func TestDialUsesTheTokenFile(t *testing.T) {
	stub := newStub()

	var got string

	original := connect
	connect = func(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (session.Session, error) {
		got = token

		return stub, nil
	}
	defer func() { connect = original }()

	t.Setenv(auth.EnvToken, "")

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(strings.Repeat("s", 32)+"\n"), 0o600); err != nil {
		t.Fatalf("writing the token file: %v", err)
	}

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), pairing.Options{TokenFile: path}, auth.TLS{}); err != nil {
		t.Fatalf("dial: %v", err)
	}

	if got != strings.Repeat("s", 32) {
		t.Errorf("the connection carried %q, want the secret from the file", got)
	}
}

// A token file that is configured but cannot be read is an error, not a reason
// to pair: the user meant to use the secret.
func TestDialRefusesAnUnreadableTokenFile(t *testing.T) {
	stub := newStub()
	restore := stubConnection(t, stub)
	defer restore()

	t.Setenv(auth.EnvToken, "")

	// A directory is not a token file, and reading it fails with something
	// other than "not exist" — which is the case that must not be swallowed.
	path := t.TempDir()

	if _, err := Dial([]string{"192.168.1.5"}, app.New(8080), pairing.Options{TokenFile: path}, auth.TLS{}); err == nil {
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
