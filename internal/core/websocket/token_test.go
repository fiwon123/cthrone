package websocketcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
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

// A host that allows anonymous connections asks for no secret. It is the
// state the tool was in before the secret existed, and it has to be asked
// for.
func TestHostAllowsAnonymousWithoutASecret(t *testing.T) {
	original := host
	defer func() { host = original }()

	var gotToken string

	var gotAnonymous bool

	host = func(port int, bindAddress, token string, allowAnonymous bool) error {
		gotToken = token
		gotAnonymous = allowAnonymous

		return nil
	}

	t.Setenv(auth.EnvToken, "")

	data := app.New(8080)
	data.AllowAnonymous = true

	if err := Host(data, "", filepath.Join(t.TempDir(), "token")); err != nil {
		t.Fatal(err)
	}

	if gotToken != "" {
		t.Errorf("expected no secret to be required, got %q", gotToken)
	}

	if !gotAnonymous {
		t.Error("expected the host to allow anonymous connections")
	}
}

// A host that requires the secret passes it on to the server.
func TestHostPassesTheSecretToTheServer(t *testing.T) {
	original := host
	defer func() { host = original }()

	var gotToken string

	host = func(port int, bindAddress, token string, allowAnonymous bool) error {
		gotToken = token

		return nil
	}

	t.Setenv(auth.EnvToken, "from-env")

	data := app.New(8080)

	if err := Host(data, "", ""); err != nil {
		t.Fatal(err)
	}

	if gotToken != "from-env" {
		t.Errorf("expected the secret to be passed on, got %q", gotToken)
	}
}
