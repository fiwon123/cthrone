package peerscmd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// newKey is a device key for a test peer.
func newKey(t *testing.T) ed25519.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return pub
}

// peerFile makes a store with one peer and returns its path and fingerprint.
func peerFile(t *testing.T, name string) (string, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "peers.json")

	store, err := discovery.NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)
	if err := store.Pin(pub, name); err != nil {
		t.Fatal(err)
	}

	return path, discovery.Fingerprint(pub)
}

// Listing names the peer and its fingerprint: the fingerprint is what the user
// compares, and the name is what forget takes.
func TestListShowsNameAndFingerprint(t *testing.T) {
	path, fingerprint := peerFile(t, "laptop")

	var out bytes.Buffer
	if err := run(nil, path, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "laptop") {
		t.Errorf("the listing does not name the peer:\n%s", text)
	}

	if !strings.Contains(text, fingerprint) {
		t.Errorf("the listing does not carry the fingerprint:\n%s", text)
	}

	if !strings.Contains(text, "FINGERPRINT") {
		t.Errorf("the listing has no header:\n%s", text)
	}
}

// An empty store says so, and says how a pin gets there. A blank screen reads
// as a broken command.
func TestListExplainsAnEmptyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")

	var out bytes.Buffer
	if err := run(nil, path, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(out.String(), "No pinned devices") {
		t.Errorf("an empty listing = %q", out.String())
	}
}

// Forgetting by name removes the pin, and a second forget says there is
// nothing to forget instead of reporting a success that did nothing.
func TestForgetRemovesThePin(t *testing.T) {
	path, _ := peerFile(t, "laptop")

	var out bytes.Buffer
	if err := run([]string{"forget", "laptop"}, path, &out); err != nil {
		t.Fatalf("forget: %v", err)
	}

	store, err := discovery.NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(store.List()); got != 0 {
		t.Fatalf("pins after forget = %d, want 0", got)
	}

	if err := run([]string{"forget", "laptop"}, path, &out); err == nil {
		t.Fatal("forgetting a missing pin reported success")
	}
}

// A fingerprint with or without its separators is the same pin.
func TestForgetAcceptsAFingerprintWithoutSeparators(t *testing.T) {
	path, fingerprint := peerFile(t, "laptop")

	var out bytes.Buffer
	if err := run([]string{"forget", strings.ReplaceAll(fingerprint, "-", "")}, path, &out); err != nil {
		t.Fatalf("forget by fingerprint: %v", err)
	}
}

// An action that is not "forget" is a usage error, not a listing with a typo
// silently ignored.
func TestUnknownActionIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")

	var out bytes.Buffer
	if err := run([]string{"remove", "laptop"}, path, &out); err == nil {
		t.Fatal("an unknown action was accepted")
	}

	if err := run([]string{"forget"}, path, &out); err == nil {
		t.Fatal("forget with no pin was accepted")
	}
}

// The flag is how a caller points the command at a file the default would not
// find — a container, or a second profile.
func TestPeersFileFlagIsRegistered(t *testing.T) {
	flag := Cmd.Flags().Lookup("peers-file")
	if flag == nil {
		t.Fatal("expected the --peers-file flag to be registered")
	}

	if flag.DefValue != "" {
		t.Errorf("expected the default file by default, got %q", flag.DefValue)
	}
}
