package auth

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

// A generated certificate carries a fingerprint: the client pins that, not
// the certificate authority, because the certificate is self-signed.
func TestGenerateCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1", "cthrone"})
	if err != nil {
		t.Fatal(err)
	}

	if cert.Fingerprint == "" {
		t.Error("expected the certificate to have a fingerprint")
	}

	if len(cert.CertPEM) == 0 || len(cert.KeyPEM) == 0 {
		t.Error("expected the certificate and the key to be written")
	}
}

// The fingerprint of a stored certificate is the fingerprint of the file:
// the host prints the one it serves, not another one.
func TestFingerprintOfFile(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := WriteCertificate(certPath, keyPath, cert); err != nil {
		t.Fatal(err)
	}

	got, err := FingerprintOfFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	if got != cert.Fingerprint {
		t.Errorf("expected %q, got %q", cert.Fingerprint, got)
	}
}

// The private key is not readable by anyone else.
func TestWriteCertificateKeepsTheKeyPrivate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")

	if err := WriteCertificate(filepath.Join(dir, "cert.pem"), keyPath, cert); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}

// The fingerprint is compared in one form, however it was written down.
func TestNormalizeFingerprint(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "AB:CD:EF", want: "abcdef"},
		{in: "ab cd ef", want: "abcdef"},
		{in: "ABCDEF", want: "abcdef"},
	}

	for _, tc := range cases {
		if got := NormalizeFingerprint(tc.in); got != tc.want {
			t.Errorf("expected %q, got %q", tc.want, got)
		}
	}
}

// A client that pins a fingerprint checks it. A certificate that does not
// match is refused.
func TestClientTLSConfigRefusesAnotherCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	other, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := ClientTLSConfig(cert.Fingerprint, false)
	if err != nil {
		t.Fatal(err)
	}

	if config.InsecureSkipVerify != true {
		t.Error("expected the normal verification to be skipped, the fingerprint is the check")
	}

	err = config.VerifyPeerCertificate([][]byte{other.DER}, nil)
	if err == nil {
		t.Fatal("expected another certificate to be refused")
	}
}

// The certificate that matches is accepted.
func TestClientTLSConfigAcceptsThePinnedCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := ClientTLSConfig(cert.Fingerprint, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Errorf("expected the pinned certificate to be accepted, got %v", err)
	}
}

// A client that is told to be insecure accepts anything — that is what the
// flag means.
func TestClientTLSConfigInsecure(t *testing.T) {
	config, err := ClientTLSConfig("", true)
	if err != nil {
		t.Fatal(err)
	}

	if config.InsecureSkipVerify != true {
		t.Error("expected the verification to be skipped")
	}

	if config.VerifyPeerCertificate != nil {
		t.Error("expected no fingerprint check when the client is insecure")
	}
}

// A client that wants TLS but names no fingerprint and does not ask to be
// insecure has to be told what to do.
func TestClientTLSConfigNeedsAFingerprint(t *testing.T) {
	if _, err := ClientTLSConfig("", false); err == nil {
		t.Error("expected an error when there is no fingerprint")
	}
}

// The configuration a client uses is a TLS configuration.
func TestClientTLSConfigIsUsable(t *testing.T) {
	config, err := ClientTLSConfig("ab12", false)
	if err != nil {
		t.Fatal(err)
	}

	var _ *tls.Config = config
}

// A fingerprint is compared in one form, however it was written down: the
// form a user copies is often the one a tool prints with colons in it.
func TestClientTLSConfigAcceptsTheFingerprintInAnotherForm(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	spaced := ""
	for i, char := range cert.Fingerprint {
		if i > 0 && i%2 == 0 {
			spaced += ":"
		}

		spaced += string(char)
	}

	config, err := ClientTLSConfig(spaced, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Errorf("expected the fingerprint to be matched, got %v", err)
	}
}
