package websocketcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
)

// A host that serves TLS needs a certificate. It generates one the first
// time and keeps it afterwards: a client that pinned the fingerprint must
// not find a different certificate tomorrow.
func TestHostCertificateIsGeneratedAndKept(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	gotCert, gotKey, err := hostCertificate(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}

	if gotCert != certFile || gotKey != keyFile {
		t.Errorf("expected %q and %q, got %q and %q", certFile, keyFile, gotCert, gotKey)
	}

	if !fileExists(certFile) {
		t.Fatal("expected the certificate to be written")
	}

	first, err := auth.FingerprintOfFile(certFile)
	if err != nil {
		t.Fatal(err)
	}

	// The second time, the certificate that is there is the one that is
	// served.
	_, _, err = hostCertificate(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}

	second, err := auth.FingerprintOfFile(certFile)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("expected the certificate to be kept, the fingerprint changed from %q to %q", first, second)
	}
}

// The certificate names the addresses of the machine it was made on, so it
// says something true.
func TestLocalHosts(t *testing.T) {
	hosts := localHosts()

	found := false
	for _, host := range hosts {
		if host == "localhost" {
			found = true
		}
	}

	if !found {
		t.Errorf("expected localhost among the names, got %v", hosts)
	}
}

// A client that wants TLS has to say how the certificate is checked: either
// it pins a fingerprint, or it asks to be insecure. A client that says
// neither would have no way to tell a host from anyone else.
func TestClientTLSConfigNeedsAFingerprintOrInsecure(t *testing.T) {
	_, err := clientTLSConfig(auth.TLS{Enabled: true})
	if err == nil {
		t.Fatal("expected an error when neither a fingerprint nor --insecure is given")
	}

	if err.Error() != "--tls needs --cert-fingerprint to check the certificate, or --insecure to skip the check" {
		t.Errorf("expected the error to name the flags, got %q", err.Error())
	}
}

// A client that pins a fingerprint gets a configuration that checks it.
func TestClientTLSConfigWithAFingerprint(t *testing.T) {
	config, err := clientTLSConfig(auth.TLS{Enabled: true, Fingerprint: "ab12"})
	if err != nil {
		t.Fatal(err)
	}

	if config == nil {
		t.Fatal("expected a TLS configuration")
	}
}

// A client that asks to be insecure gets a configuration that checks
// nothing.
func TestClientTLSConfigInsecure(t *testing.T) {
	config, err := clientTLSConfig(auth.TLS{Enabled: true, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}

	if config == nil {
		t.Fatal("expected a TLS configuration")
	}
}

// A client that does not use TLS gets no TLS configuration: the transport
// is not encrypted and nothing pretends otherwise.
func TestClientTLSConfigWithoutTLS(t *testing.T) {
	config, err := clientTLSConfig(auth.TLS{})
	if err != nil {
		t.Fatal(err)
	}

	if config != nil {
		t.Error("expected no TLS configuration when TLS is off")
	}
}

// The host serves the certificate it generated, and the key stays private.
func TestHostServesTheCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if _, _, err := hostCertificate(certFile, keyFile); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}
