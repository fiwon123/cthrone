package websocketcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/session"
)

// A host with no secret gets one: the two devices need the same secret and
// the only way to get it is to read it.
func TestHostTokenGeneratesOneWhenThereIsNone(t *testing.T) {
	t.Setenv(auth.EnvToken, "")

	file := filepath.Join(t.TempDir(), "token")

	secret, err := hostToken(tokenConfig{filePath: file})
	if err != nil {
		t.Fatal(err)
	}

	if secret == "" {
		t.Fatal("expected a secret to be generated")
	}

	stored, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("expected the secret to be stored in %s: %v", file, err)
	}

	if len(stored) == 0 {
		t.Error("expected the secret to be written to the file")
	}
}

// The secret that is already there is used, not replaced. Two devices that
// share a secret file must not disagree after the host starts.
func TestHostTokenKeepsTheSecretThatIsThere(t *testing.T) {
	t.Setenv(auth.EnvToken, "")

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("already-there\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	secret, err := hostToken(tokenConfig{filePath: file})
	if err != nil {
		t.Fatal(err)
	}

	if secret != "already-there" {
		t.Errorf("expected the existing secret to be kept, got %q", secret)
	}
}

// The client reads the same sources the host does.
func TestClientTokenReadsTheFile(t *testing.T) {
	t.Setenv(auth.EnvToken, "")

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("already-there\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	secret, err := clientToken(tokenConfig{filePath: file})
	if err != nil {
		t.Fatal(err)
	}

	if secret != "already-there" {
		t.Errorf("expected the secret to be read, got %q", secret)
	}
}

// The environment is the source a container uses.
func TestClientTokenReadsTheEnvironment(t *testing.T) {
	t.Setenv(auth.EnvToken, "from-env")

	secret, err := clientToken(tokenConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if secret != "from-env" {
		t.Errorf("expected the environment to be read, got %q", secret)
	}
}

// A host passes the secret it resolved to the server: the handler checks it
// on every client, and a secret the handler never saw would refuse everyone.
func TestOpenServerPassesTheSecretToTheHandler(t *testing.T) {
	original := server
	defer func() { server = original }()

	var gotToken string

	server = func(port int, bindAddress, token, certFile, keyFile string, onSession func(session.Session)) (*websockethandler.Server, error) {
		gotToken = token

		return &websockethandler.Server{}, nil
	}

	t.Setenv(auth.EnvToken, "from-env")

	data := app.New(8080)

	handle, err := OpenServer(data, "", "", auth.TLS{}, func(session.Session) {})
	if err != nil {
		t.Fatal(err)
	}

	if gotToken != "from-env" {
		t.Errorf("expected the secret to be passed on, got %q", gotToken)
	}

	if handle.Token != "from-env" {
		t.Errorf("expected the handle to report the secret, got %q", handle.Token)
	}
}

// Every client the server lets in reaches the caller already sealed: the
// wrapper is installed here, so a caller cannot forget it.
func TestOpenServerSealsEverySession(t *testing.T) {
	original := server
	defer func() { server = original }()

	captured := make(chan session.Session, 1)

	server = func(port int, bindAddress, token, certFile, keyFile string, onSession func(session.Session)) (*websockethandler.Server, error) {
		stub := newStub()

		// What the handler does for every client: hand the raw session over.
		onSession(stub)

		if len(stub.outgoing()) == 0 {
			t.Error("the wrapper sent no handshake")
		}

		return &websockethandler.Server{}, nil
	}

	t.Setenv(auth.EnvToken, strings.Repeat("s", 32))

	data := app.New(8080)

	if _, err := OpenServer(data, "", "", auth.TLS{}, func(s session.Session) { captured <- s }); err != nil {
		t.Fatal(err)
	}

	select {
	case s := <-captured:
		if _, ok := s.(*e2e.Conn); !ok {
			t.Fatalf("the caller received %T, want the sealed session", s)
		}
	case <-time.After(time.Second):
		t.Fatal("the caller never received the session")
	}
}

// A secret too short to seal with is refused on the host side too: the client
// is dropped rather than served in the clear.
func TestOpenServerRefusesAShortSecret(t *testing.T) {
	original := server
	defer func() { server = original }()

	var stub *stubSession

	server = func(port int, bindAddress, token, certFile, keyFile string, onSession func(session.Session)) (*websockethandler.Server, error) {
		stub = newStub()
		onSession(stub)

		return &websockethandler.Server{}, nil
	}

	// A secret this short cannot seal anything, and the tool must not guess.
	if _, err := OpenServer(app.New(8080), "short", "", auth.TLS{}, func(session.Session) {}); err != nil {
		t.Skip("the secret was refused before a client arrived:", err)
	}

	if stub == nil {
		t.Fatal("the server was never asked to serve")
	}

	if !stub.IsClosed() {
		t.Error("expected the unsealable session to be closed")
	}
}
