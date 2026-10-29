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

// newCertStore makes an empty store and returns it with its path, so a test
// can check what a later run would read.
func newCertStore(t *testing.T) (*PinStore, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "peers.json")

	store, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	return store, path
}

// A certificate is checked without being written — the check runs during the
// TLS handshake, and the pin belongs to the moment the certificate arrives —
// and the first one at an address is then pinned.
func TestPinCertRecordsOnFirstContact(t *testing.T) {
	store, path := newCertStore(t)
	fingerprint := strings.Repeat("ab", 32)

	first, err := store.CheckCert("192.168.1.5:8080", fingerprint)
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Fatal("expected the first contact to be reported as one")
	}

	if got := len(store.Certificates()); got != 0 {
		t.Fatalf("check recorded %d certificates, want 0", got)
	}

	if err := store.PinCert("192.168.1.5:8080", fingerprint); err != nil {
		t.Fatal(err)
	}

	first, err = store.CheckCert("192.168.1.5:8080", strings.ToUpper(fingerprint))
	if err != nil {
		t.Fatalf("the pinned certificate was refused: %v", err)
	}

	if first {
		t.Error("a pinned address was reported as a first contact")
	}

	// The pin is on disk, in the form the next run reads: the file is public
	// keys only, but still 0600.
	reloaded, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	certs := reloaded.Certificates()
	if len(certs) != 1 {
		t.Fatalf("the next run read %d certificates, want 1", len(certs))
	}

	if certs[0].Address != "192.168.1.5:8080" || certs[0].Fingerprint != fingerprint {
		t.Errorf("pinned %+v, want %q with %q", certs[0], "192.168.1.5:8080", fingerprint)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the pins are %o, want 600", perm)
	}
}

// A different certificate at a pinned address is refused rather than trusted,
// and the pin stays as it was until the user forgets it.
func TestCheckCertRefusesAChangedCertificate(t *testing.T) {
	store, _ := newCertStore(t)

	first := strings.Repeat("aa", 32)
	other := strings.Repeat("bb", 32)

	if err := store.PinCert("192.168.1.5:8080", first); err != nil {
		t.Fatal(err)
	}

	if _, err := store.CheckCert("192.168.1.5:8080", other); !errors.Is(err, ErrCertificateChanged) {
		t.Fatalf("a changed certificate = %v, want ErrCertificateChanged", err)
	}

	if err := store.PinCert("192.168.1.5:8080", other); !errors.Is(err, ErrCertificateChanged) {
		t.Fatalf("pinning a changed certificate = %v, want ErrCertificateChanged", err)
	}

	certs := store.Certificates()
	if len(certs) != 1 || certs[0].Fingerprint != first {
		t.Errorf("the pin changed to %+v", certs)
	}
}

// Forgetting is the way back from a reinstalled host: an address without its
// port names the pin the client recorded, and a fingerprint works too.
func TestForgetRemovesACertificateByAddressOrFingerprint(t *testing.T) {
	store, _ := newCertStore(t)

	laptop := strings.Repeat("aa", 32)
	phone := strings.Repeat("bb", 32)

	if err := store.PinCert("192.168.1.5:8080", laptop); err != nil {
		t.Fatal(err)
	}

	if err := store.PinCert("192.168.1.6:9000", phone); err != nil {
		t.Fatal(err)
	}

	forgot, err := store.Forget("192.168.1.5")
	if err != nil || !forgot {
		t.Fatalf("forget by address = %v, %v; want the pin removed", forgot, err)
	}

	if got := store.Certificates(); len(got) != 1 || got[0].Address != "192.168.1.6:9000" {
		t.Fatalf("certificates after forget = %+v, want only 192.168.1.6:9000", got)
	}

	forgot, err = store.Forget(strings.ToUpper(phone))
	if err != nil || !forgot {
		t.Fatalf("forget by fingerprint = %v, %v; want the pin removed", forgot, err)
	}

	if got := store.Certificates(); len(got) != 0 {
		t.Errorf("certificates after forget = %+v, want none", got)
	}

	forgot, err = store.Forget("192.168.1.5")
	if err != nil || forgot {
		t.Errorf("forget of a missing pin = %v, %v; want neither", forgot, err)
	}
}

// One host can be reached on more than one port, and each port gets its own
// pin. Forgetting by host forgets all of them: the user remembers the host,
// not the port the tool appended, and a pin left behind would keep refusing
// the certificate the user just said to forget.
func TestForgetRemovesEveryCertificateUnderAHost(t *testing.T) {
	store, _ := newCertStore(t)

	for _, address := range []string{"192.168.1.5:8080", "192.168.1.5:9000"} {
		if err := store.PinCert(address, strings.Repeat("ab", 32)); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.PinCert("192.168.1.6:8080", strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}

	forgot, err := store.Forget("192.168.1.5")
	if err != nil || !forgot {
		t.Fatalf("forget by host = %v, %v; want the pins removed", forgot, err)
	}

	got := store.Certificates()
	if len(got) != 1 || got[0].Address != "192.168.1.6:8080" {
		t.Errorf("certificates after forget = %+v, want only the other host", got)
	}
}

// A pin of each kind lives in one file, and forgetting one leaves the other:
// the device pin and the certificate pin answer different questions.
func TestDeviceAndCertificatePinsShareTheFile(t *testing.T) {
	store, path := newCertStore(t)

	pub := newKey(t)
	if err := store.Pin(pub, "laptop"); err != nil {
		t.Fatal(err)
	}

	if err := store.PinCert("192.168.1.5:8080", strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}

	if forgot, err := store.Forget("laptop"); err != nil || !forgot {
		t.Fatalf("forget by name = %v, %v; want the device pin removed", forgot, err)
	}

	reloaded, err := NewPinStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(reloaded.List()); got != 0 {
		t.Errorf("device pins after forget = %d, want 0", got)
	}

	if got := len(reloaded.Certificates()); got != 1 {
		t.Errorf("certificate pins after forget = %d, want 1", got)
	}
}
