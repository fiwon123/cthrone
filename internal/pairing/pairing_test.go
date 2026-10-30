package pairing

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
)

// A shared secret is read from the same three places the transports always
// read it from, and the flag wins because it is the most explicit.
func TestLoadReadsTheSharedSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("  from-file-secret-01\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		opts Options
		env  string
		want string
	}{
		{
			name: "the flag wins",
			opts: Options{FlagToken: "from-flag-secret-01", TokenFile: file},
			env:  "from-env-secret-001",
			want: "from-flag-secret-01",
		},
		{
			name: "the environment beats the file",
			opts: Options{TokenFile: file},
			env:  "from-env-secret-001",
			want: "from-env-secret-001",
		},
		{
			name: "the file is trimmed",
			opts: Options{TokenFile: file},
			want: "from-file-secret-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(auth.EnvToken, tt.env)

			cred, err := Load(tt.opts)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if !cred.HasSecret() {
				t.Fatal("a configured secret did not produce a secret credential")
			}

			if cred.Secret() != tt.want {
				t.Errorf("secret = %q, want %q", cred.Secret(), tt.want)
			}

			if cred.Fingerprint() != "" {
				t.Errorf("a secret credential reports the fingerprint %q", cred.Fingerprint())
			}
		})
	}
}

// A secret too short to derive a key from is refused where it is read, not
// when the first peer arrives: a host that accepted it would fail inside its
// per-connection wrapper, where nobody reads the error, and drop every client
// in silence.
func TestLoadRefusesASecretTooShortForTheHandshake(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	_, err := Load(Options{FlagToken: "too-short"})
	if !errors.Is(err, e2e.ErrSecret) {
		t.Fatalf("Load with a short secret = %v, want ErrSecret", err)
	}
}

// No secret anywhere means this device's own key: the credential has no
// secret to copy, and it reports a fingerprint another screen can show.
func TestLoadUsesTheDeviceKeyWhenThereIsNoSecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(auth.EnvToken, "")

	cred, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cred.HasSecret() {
		t.Fatal("a credential with no configured secret is a shared secret")
	}

	if cred.Source() != SourceDeviceKey {
		t.Errorf("source = %q, want %q", cred.Source(), SourceDeviceKey)
	}

	if cred.Fingerprint() == "" {
		t.Fatal("the device-key credential reports no fingerprint")
	}

	info, err := os.Stat(filepath.Join(home, ".config", "cthrone", "identity.key"))
	if err != nil {
		t.Fatalf("the key was not written: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the key is %o, want 600", mode)
	}
}

// The two models are exclusive in the e2e configuration: a device-key
// credential speaks the identity handshake and leaves Secret empty.
func TestConfigMatchesTheCredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	secret, err := Load(Options{FlagToken: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}

	cfg := secret.Config(e2e.Bus, "alice")
	if len(cfg.Secret) == 0 || cfg.Identity != nil || cfg.Pins != nil {
		t.Fatalf("a secret credential configured %+v", cfg)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	identity, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}

	cfg = identity.Config(e2e.Single, "alice")
	if len(cfg.Secret) != 0 || cfg.Identity == nil || cfg.Pins == nil {
		t.Fatalf("a device-key credential configured %+v", cfg)
	}

	if _, err := e2e.New(newPipe(), cfg); err != nil {
		t.Fatalf("the device-key configuration is not one e2e accepts: %v", err)
	}
}

// A caller that names its own files gets them: the GUI has no ~/.config to
// rely on, so it must be able to put the key and the pins in its own storage.
func TestLoadHonoursExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	keyFile := filepath.Join(dir, "app-identity.key")
	pinsFile := filepath.Join(dir, "app-peers.json")

	cred, err := Load(Options{
		TokenFile: filepath.Join(dir, "app-token"),
		KeyFile:   keyFile,
		PinsFile:  pinsFile,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cred.HasSecret() {
		t.Fatal("a nonexistent token file produced a secret")
	}

	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("the key was not written where it was asked for: %v", err)
	}

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "cthrone")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the default directory was touched: %v", err)
	}
}

// A secret that is configured but unreadable is an error. Falling back to
// pairing would pin a device the user never asked to pin.
func TestLoadRefusesAnUnreadableSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	if _, err := Load(Options{TokenFile: t.TempDir()}); err == nil {
		t.Fatal("a directory as the token file was treated as no secret")
	}
}

// A key file that is not a key is an error, never a new identity.
func TestLoadRefusesACorruptKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	keyFile := filepath.Join(dir, "identity.key")
	if err := os.WriteFile(keyFile, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(Options{TokenFile: filepath.Join(dir, "no-token"), KeyFile: keyFile}); err == nil {
		t.Fatal("a corrupt key was replaced with a new identity")
	}
}

// The fingerprint is the public half of the stored key, in the comparable form
// the pin store uses.
func TestFingerprintIsTheStoredKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(auth.EnvToken, "")

	cred, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(filepath.Join(home, ".config", "cthrone", "identity.key"))
	if err != nil {
		t.Fatal(err)
	}

	seed, err := hex.DecodeString(strings.TrimSpace(string(stored)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("the key file is not a hex seed: %v", err)
	}

	pub, _ := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if cred.Fingerprint() != discovery.Fingerprint(pub) {
		t.Fatalf("fingerprint = %q, want the stored key's", cred.Fingerprint())
	}
}

// The pins are loaded in both models. The host certificate is trusted on
// first use before the handshake says which credential the peer speaks, so a
// session that authenticates with a shared secret still needs them.
func TestPinsAreLoadedInBothModels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	pinsFile := filepath.Join(dir, "peers.json")

	secret, err := Load(Options{FlagToken: strings.Repeat("s", 32), PinsFile: pinsFile})
	if err != nil {
		t.Fatal(err)
	}

	if secret.Pins() == nil {
		t.Error("a shared-secret credential has no pin store")
	}

	identity, err := Load(Options{
		TokenFile: filepath.Join(dir, "no-token"),
		KeyFile:   filepath.Join(dir, "identity.key"),
		PinsFile:  pinsFile,
	})
	if err != nil {
		t.Fatal(err)
	}

	if identity.Pins() == nil {
		t.Error("a device-key credential has no pin store")
	}
}

// stub is the smallest session e2e.New needs: it sends the hello into a buffer
// and nothing has to answer.
type stub struct {
	*session.Base

	sent [][]byte
}

func newPipe() *stub {
	return &stub{Base: session.NewBase("stub", "stub.example:8080")}
}

func (s *stub) Send(payload []byte) error {
	s.sent = append(s.sent, append([]byte(nil), payload...))

	return nil
}

func (s *stub) Close(reason string) { s.NotifyClose(reason) }

// A status line has to name the credential and where it came from, because a
// token file nobody remembers writing is what switches a device into the
// shared-secret model and makes it refuse a key-paired peer.
func TestDescribeNamesTheCredentialAndItsSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("f", 32)), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		opts     Options
		env      string
		want     string
		secret   string
		wantFile string
	}{
		{
			name:     "the flag",
			opts:     Options{FlagToken: strings.Repeat("a", 32)},
			want:     "--token",
			secret:   strings.Repeat("a", 32),
			wantFile: "",
		},
		{
			name:     "the environment",
			opts:     Options{TokenFile: filepath.Join(dir, "absent")},
			env:      strings.Repeat("b", 32),
			want:     auth.EnvToken,
			secret:   strings.Repeat("b", 32),
			wantFile: "",
		},
		{
			name:     "the file",
			opts:     Options{TokenFile: tokenFile},
			want:     tokenFile,
			secret:   strings.Repeat("f", 32),
			wantFile: tokenFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(auth.EnvToken, tt.env)

			cred, err := Load(tt.opts)
			if err != nil {
				t.Fatal(err)
			}

			described := cred.Describe()

			if !strings.Contains(described, "shared secret") {
				t.Errorf("Describe() = %q, want it to name the shared secret", described)
			}

			if !strings.Contains(described, tt.want) {
				t.Errorf("Describe() = %q, want it to name %q", described, tt.want)
			}

			if strings.Contains(described, tt.secret) {
				t.Errorf("Describe() carries the secret itself: %q", described)
			}

			if got := cred.SecretFile(); got != tt.wantFile {
				t.Errorf("SecretFile() = %q, want %q", got, tt.wantFile)
			}
		})
	}
}

// The device-key model is described by the key's fingerprint, which is public
// and is the value the other screen compares. There is no secret to name, and
// no file to offer to delete.
func TestDescribeNamesTheDeviceKeyByItsFingerprint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	cred, err := Load(Options{
		TokenFile: filepath.Join(dir, "absent"),
		KeyFile:   filepath.Join(dir, "identity.key"),
		PinsFile:  filepath.Join(dir, "peers.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	described := cred.Describe()

	if !strings.Contains(described, SourceDeviceKey) {
		t.Errorf("Describe() = %q, want it to name the device key", described)
	}

	if !strings.Contains(described, cred.Fingerprint()) {
		t.Errorf("Describe() = %q, want it to carry the fingerprint %q", described, cred.Fingerprint())
	}

	if got := cred.SecretFile(); got != "" {
		t.Errorf("SecretFile() = %q, want none: the device key is not a file to delete", got)
	}
}

// The line a command prints describes the credential the transport will use,
// and says so when there is no usable one instead of staying quiet until the
// transport fails.
func TestDescribeOptionsMatchesTheLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(auth.EnvToken, "")

	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 32)), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := Options{TokenFile: tokenFile}

	cred, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := DescribeOptions(opts), cred.Describe(); got != want {
		t.Errorf("DescribeOptions() = %q, want %q", got, want)
	}

	// A short flag secret is refused by Load, and the line has to carry that
	// reason: the transport reports the same error, and this is the earlier
	// one the user actually reads.
	bad := Options{FlagToken: "short"}
	if _, err := Load(bad); err == nil {
		t.Fatal("a short flag secret was accepted")
	}

	if got := DescribeOptions(bad); !strings.HasPrefix(got, "unavailable") {
		t.Errorf("DescribeOptions() = %q, want it to report the failure", got)
	}
}
