package discovery

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeyCreatesThenKeepsTheKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := LoadKey()
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != ed25519.PrivateKeySize {
		t.Fatalf("key is %d bytes, want %d", len(first), ed25519.PrivateKeySize)
	}

	path, err := IdentityKeyFile()
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the key file was not written: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("key file mode = %o, want 600", mode)
	}

	second, err := LoadKey()
	if err != nil {
		t.Fatal(err)
	}

	if !first.Equal(second) {
		t.Fatal("a second load made a new identity")
	}
}

func TestLoadKeyRefusesACorruptFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := IdentityKeyFile()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadKey(); err == nil {
		t.Fatal("a corrupt key file was silently replaced with a new identity")
	}
}
