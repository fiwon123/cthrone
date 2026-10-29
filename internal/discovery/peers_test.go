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

func TestAcceptPinsOnFirstContact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")

	store, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	pub := newKey(t)

	fingerprint, first, err := store.Accept(pub, "laptop")
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Error("the first contact was not reported as first")
	}

	if fingerprint != Fingerprint(pub) {
		t.Errorf("fingerprint = %q, want %q", fingerprint, Fingerprint(pub))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the pins were not written: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("pin file mode = %o, want 600", mode)
	}

	// The same device again is known, not new.
	if _, first, err := store.Accept(pub, "laptop"); err != nil || first {
		t.Fatalf("Accept again = first %v, err %v; want known", first, err)
	}

	// And it survived a reload: the pin is on disk, not just in memory.
	reloaded, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, first, err := reloaded.Accept(pub, "laptop"); err != nil || first {
		t.Fatalf("Accept after reload = first %v, err %v; want known", first, err)
	}
}

func TestAcceptRefusesAChangedKeyUnderTheSameName(t *testing.T) {
	store, err := NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Accept(newKey(t), "laptop"); err != nil {
		t.Fatal(err)
	}

	_, _, err = store.Accept(newKey(t), "laptop")
	if !errors.Is(err, ErrIdentityChanged) {
		t.Fatalf("Accept with a new key = %v, want ErrIdentityChanged", err)
	}
}

func TestAcceptKeysAnUnnamedPeerByFingerprint(t *testing.T) {
	store, err := NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	first, second := newKey(t), newKey(t)

	if _, isFirst, err := store.Accept(first, ""); err != nil || !isFirst {
		t.Fatalf("first unnamed peer = first %v, err %v", isFirst, err)
	}

	// Two unnamed devices are two identities, not one changed one.
	if _, isFirst, err := store.Accept(second, ""); err != nil || !isFirst {
		t.Fatalf("second unnamed peer = first %v, err %v", isFirst, err)
	}

	if _, isFirst, err := store.Accept(first, ""); err != nil || isFirst {
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
	fingerprint, _, err := store.Accept(pub, "laptop")
	if err != nil {
		t.Fatal(err)
	}

	// A fingerprint survives a trip through a shell that dropped the dashes.
	if !store.Forget(strings.ReplaceAll(fingerprint, "-", "")) {
		t.Fatal("forget by fingerprint did not find the pin")
	}

	if got := len(store.List()); got != 0 {
		t.Fatalf("pins = %d after forget, want 0", got)
	}

	if _, first, err := store.Accept(pub, "laptop"); err != nil || !first {
		t.Fatalf("after forget = first %v, err %v; want a new first contact", first, err)
	}

	if !store.Forget("laptop") {
		t.Fatal("forget by name did not find the pin")
	}

	if store.Forget("laptop") {
		t.Error("forget reported removing a pin that was not there")
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
