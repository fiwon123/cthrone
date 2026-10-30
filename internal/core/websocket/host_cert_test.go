package websocketcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
)

// withoutUserConfigDir removes both variables os.UserConfigDir reads, so the
// platform looks like Android: Fyne's Android runtime copies FILESDIR, TMPDIR,
// PATH and LD_LIBRARY_PATH into the Go environment and no HOME, so
// os.UserConfigDir returns an error there.
func withoutUserConfigDir(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	if _, err := os.UserConfigDir(); err == nil {
		t.Fatal("os.UserConfigDir still resolves; the test cannot reproduce Android")
	}
}

// A caller that names the certificate files does not depend on the platform's
// user configuration directory. The GUI on a phone passes paths in the app's
// own storage because Android has no $HOME; hosting used to fail there before
// the listener bound, because the default was resolved before the configured
// paths were looked at.
func TestHostCertificateUsesConfiguredPathsWithoutAConfigDir(t *testing.T) {
	withoutUserConfigDir(t)

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	gotCert, gotKey, err := hostCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("hostCertificate with configured paths: %v", err)
	}

	if gotCert != certFile {
		t.Errorf("cert path = %q, want %q", gotCert, certFile)
	}

	if gotKey != keyFile {
		t.Errorf("key path = %q, want %q", gotKey, keyFile)
	}

	for _, path := range []string{certFile, keyFile} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written: %v", path, err)
		}
	}

	// The files have to be a usable certificate pair, not just bytes: the
	// fingerprint is what the host shows the other device.
	if _, err := auth.FingerprintOfFile(certFile); err != nil {
		t.Errorf("the written certificate cannot be read back: %v", err)
	}

	// A second host run reuses the pair, so a device that pinned the
	// fingerprint does not find a different certificate tomorrow.
	if _, _, err := hostCertificate(certFile, keyFile); err != nil {
		t.Fatalf("hostCertificate on an existing pair: %v", err)
	}
}

// Without configured paths the default is the platform's configuration
// directory. A platform that has none is an error rather than a key written
// somewhere the next run cannot find again.
func TestHostCertificateNeedsAConfigDirWithoutConfiguredPaths(t *testing.T) {
	withoutUserConfigDir(t)

	if _, _, err := hostCertificate("", ""); err == nil {
		t.Fatal("hostCertificate(\"\", \"\") succeeded without a user configuration directory")
	}
}

// A caller that names one path still gets the default for the other, so a
// half-configured TLS setting keeps working where a default exists.
func TestHostCertificateFillsOnlyTheMissingPath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", "")

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")

	gotCert, gotKey, err := hostCertificate(certFile, "")
	if err != nil {
		t.Fatalf("hostCertificate with a configured certificate: %v", err)
	}

	if gotCert != certFile {
		t.Errorf("cert path = %q, want %q", gotCert, certFile)
	}

	wantKey := filepath.Join(configHome, "cthrone", "key.pem")
	if gotKey != wantKey {
		t.Errorf("key path = %q, want %q", gotKey, wantKey)
	}

	if _, err := os.Stat(gotKey); err != nil {
		t.Errorf("%s was not written: %v", gotKey, err)
	}
}
