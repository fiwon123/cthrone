package natscore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// stubSession is a transport session that records what the wrapper sends. The
// seam under test only needs session.Session, so no broker is involved.
type stubSession struct {
	*session.Base

	mu   sync.Mutex
	sent [][]byte

	closeReason string
}

func newStub() *stubSession {
	return &stubSession{Base: session.NewBase("bus", "")}
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

func (s *stubSession) Close(reason string) {
	s.mu.Lock()
	s.closeReason = reason
	s.mu.Unlock()

	s.NotifyClose(reason)
}

func (s *stubSession) outgoing() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]byte(nil), s.sent...)
}

func (s *stubSession) reason() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeReason
}

// A NATS session is sealed in Bus mode: several peers may hold the secret, and
// a refusal drops the peer instead of ending the room.
func TestSealWrapsInBusMode(t *testing.T) {
	stub := newStub()

	sealed, err := seal(stub, strings.Repeat("s", 32), "alice")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, ok := sealed.(*e2e.Conn); !ok {
		t.Fatalf("got %T, want the sealed session", sealed)
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

// A secret too short to seal with closes the broker session instead of leaving
// the caller with a session it might talk in the clear on.
func TestSealRefusesAShortSecret(t *testing.T) {
	stub := newStub()

	if _, err := seal(stub, "short", "alice"); err == nil {
		t.Fatal("expected a short secret to be refused")
	}

	if !stub.IsClosed() {
		t.Error("expected the broker session to be closed")
	}

	if stub.reason() == "" {
		t.Error("expected the close to say why")
	}
}

// The NATS path reads the shared secret from the same three places the
// websocket path does, so one secret protects both transports.
func TestSharedSecretSources(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("  from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		flag    string
		file    string
		env     string
		want    string
		wantErr bool
	}{
		{name: "flag wins", flag: "from-flag", file: file, env: "from-env", want: "from-flag"},
		{name: "environment beats the file", file: file, env: "from-env", want: "from-env"},
		{name: "file is trimmed", file: file, want: "from-file"},
		{name: "nothing anywhere", want: ""},
		{name: "missing file is no secret, not an error", file: "does-not-exist", want: ""},
		{name: "a file that cannot be read is an error", file: t.TempDir(), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(auth.EnvToken, tt.env)

			got, err := sharedSecret(tt.flag, tt.file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sharedSecret() error = %v, wantErr %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("sharedSecret() = %q, want %q", got, tt.want)
			}
		})
	}
}
