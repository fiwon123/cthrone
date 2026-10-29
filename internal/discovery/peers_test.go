package discovery

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKey(t *testing.T) ed25519.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return pub
}

// A check answers a question; it must not write. A hello can name any device
// and sign it with a key of its own, so a pin recorded from one would let a
// stranger claim a name and lock its owner out.
func TestCheckWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")

	store, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)

	fingerprint, first, err := store.Check(pub, "laptop")
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Error("an unknown name was not reported as a first contact")
	}

	if fingerprint != Fingerprint(pub) {
		t.Errorf("fingerprint = %q, want %q", fingerprint, Fingerprint(pub))
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Check wrote the pin file: %v", err)
	}

	if got := len(store.List()); got != 0 {
		t.Errorf("Check recorded %d pins, want 0", got)
	}
}

func TestPinRecordsOnFirstContact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")

	store, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)

	if err := store.Pin(pub, "laptop"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the pins were not written: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("pin file mode = %o, want 600", mode)
	}

	// The same device again is known, not new.
	if _, first, err := store.Check(pub, "laptop"); err != nil || first {
		t.Fatalf("Check again = first %v, err %v; want known", first, err)
	}

	// And it survived a reload: the pin is on disk, not just in memory.
	reloaded, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, first, err := reloaded.Check(pub, "laptop"); err != nil || first {
		t.Fatalf("Check after reload = first %v, err %v; want known", first, err)
	}
}

func TestPinAndCheckRefuseAChangedKeyUnderTheSameName(t *testing.T) {
	store, err := NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Pin(newKey(t), "laptop"); err != nil {
		t.Fatal(err)
	}

	other := newKey(t)

	if _, _, err := store.Check(other, "laptop"); !errors.Is(err, ErrIdentityChanged) {
		t.Fatalf("Check with a new key = %v, want ErrIdentityChanged", err)
	}

	if err := store.Pin(other, "laptop"); !errors.Is(err, ErrIdentityChanged) {
		t.Fatalf("Pin with a new key = %v, want ErrIdentityChanged", err)
	}
}

// A pin that cannot be written is reported, and does not linger in memory as
// a trust decision the next run would not know about.
func TestPinReportsAWriteThatFailed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := NewPinStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Replace the store's directory with a file, so the next write cannot
	// create its temporary file. This fails for a reason the test controls,
	// rather than depending on the permissions the test happens to run with.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)

	if err := store.Pin(pub, "laptop"); err == nil {
		t.Fatal("Pin reported success with an unwritable pin file")
	}

	if got := len(store.List()); got != 0 {
		t.Errorf("a failed pin left %d entries in memory, want 0", got)
	}

	if _, first, err := store.Check(pub, "laptop"); err != nil || !first {
		t.Errorf("after a failed pin, Check = first %v, err %v; want a first contact", first, err)
	}
}

func TestPinKeysAnUnnamedPeerByFingerprint(t *testing.T) {
	store, err := NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	first, second := newKey(t), newKey(t)

	if err := store.Pin(first, ""); err != nil {
		t.Fatal(err)
	}

	// Two unnamed devices are two identities, not one changed one.
	if err := store.Pin(second, ""); err != nil {
		t.Fatalf("second unnamed peer: %v", err)
	}

	if _, isFirst, err := store.Check(first, ""); err != nil || isFirst {
		t.Fatalf("known unnamed peer = first %v, err %v; want known", isFirst, err)
	}

	if got := len(store.List()); got != 2 {
		t.Errorf("pins = %d, want 2", got)
	}
}

func TestForgetRemovesByNameOrFingerprint(t *testing.T) {
	store, err := NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)

	if err := store.Pin(pub, "laptop"); err != nil {
		t.Fatal(err)
	}

	fingerprint := Fingerprint(pub)

	// A fingerprint survives a trip through a shell that dropped the dashes.
	forgot, err := store.Forget(strings.ReplaceAll(fingerprint, "-", ""))
	if err != nil || !forgot {
		t.Fatalf("forget by fingerprint = %v, %v; want the pin removed", forgot, err)
	}

	if got := len(store.List()); got != 0 {
		t.Fatalf("pins = %d after forget, want 0", got)
	}

	if _, first, err := store.Check(pub, "laptop"); err != nil || !first {
		t.Fatalf("after forget = first %v, err %v; want a new first contact", first, err)
	}

	if err := store.Pin(pub, "laptop"); err != nil {
		t.Fatal(err)
	}

	forgot, err = store.Forget("laptop")
	if err != nil || !forgot {
		t.Fatalf("forget by name = %v, %v; want the pin removed", forgot, err)
	}

	forgot, err = store.Forget("laptop")
	if err != nil || forgot {
		t.Errorf("forget of a missing pin = %v, %v; want neither", forgot, err)
	}
}

func TestFingerprintIsGroupedHex(t *testing.T) {
	fingerprint := Fingerprint(newKey(t))

	if got := len(fingerprint); got != 39 {
		t.Fatalf("fingerprint %q is %d characters, want 39", fingerprint, got)
	}

	for _, group := range strings.Split(fingerprint, "-") {
		if len(group) != 4 {
			t.Fatalf("fingerprint %q has a group of %d characters", fingerprint, len(group))
		}
	}
}
