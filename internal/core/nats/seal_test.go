package natscore

import (
	"strings"
	"sync"
	"testing"

	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/pairing"
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

	sealed, err := seal(stub, secretCredential(t, strings.Repeat("s", 32)), "alice")
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

// A session the wrapper cannot seal is closed instead of leaving the caller
// with one it might talk in the clear on. A short secret no longer reaches
// here — pairing.Load refuses it when the credential is resolved, where the
// error can still be reported — so what this covers is a credential a caller
// built by hand.
func TestSealClosesASessionItCannotSeal(t *testing.T) {
	stub := newStub()

	if _, err := seal(stub, pairing.Credential{}, "alice"); err == nil {
		t.Fatal("expected an unusable credential to be refused")
	}

	if !stub.IsClosed() {
		t.Error("expected the broker session to be closed")
	}

	if stub.reason() == "" {
		t.Error("expected the close to say why")
	}
}

// secretCredential is the shared-secret credential the transport tests use.
func secretCredential(t *testing.T, secret string) pairing.Credential {
	t.Helper()

	cred, err := pairing.Load(pairing.Options{FlagToken: secret})
	if err != nil {
		t.Fatalf("resolving the credential: %v", err)
	}

	return cred
}

// A NATS session with no secret anywhere is sealed with this device's key, in
// Bus mode, so one peer can be dropped without ending the room.
func TestSealUsesTheDeviceKeyWhenThereIsNoSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CTHRONE_TOKEN", "")

	cred, err := pairing.Load(pairing.Options{})
	if err != nil {
		t.Fatalf("resolving the credential: %v", err)
	}

	if cred.HasSecret() {
		t.Fatal("a credential with no configured secret is a shared secret")
	}

	stub := newStub()

	sealed, err := seal(stub, cred, "alice")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, ok := sealed.(*e2e.Conn); !ok {
		t.Fatalf("got %T, want the sealed session", sealed)
	}
}
