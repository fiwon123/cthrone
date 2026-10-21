package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Certificate is a key pair and the fingerprint of its certificate.
//
// The fingerprint is what the client pins: the certificate is self-signed, so
// there is no authority to check it against. The client checks the
// fingerprint instead, which is what a pin is.
type Certificate struct {
	CertPEM     []byte
	KeyPEM      []byte
	DER         []byte
	Fingerprint string
}

// GenerateCertificate returns a self-signed certificate for the given hosts.
// The hosts are the names the certificate is valid for; the client does not
// check them, because the fingerprint is the check.
func GenerateCertificate(hosts []string) (Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, fmt.Errorf("generating a key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return Certificate{}, fmt.Errorf("generating a serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"cthrone"}, CommonName: "cthrone"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)

			continue
		}

		template.DNSNames = append(template.DNSNames, host)
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return Certificate{}, fmt.Errorf("creating the certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Certificate{}, fmt.Errorf("encoding the key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return Certificate{
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		DER:         der,
		Fingerprint: FingerprintOfDER(der),
	}, nil
}

// FingerprintOfDER returns the fingerprint of a certificate: the SHA-256 of
// its bytes, in hex.
func FingerprintOfDER(der []byte) string {
	sum := sha256.Sum256(der)

	return hex.EncodeToString(sum[:])
}

// FingerprintOfFile returns the fingerprint of the certificate in the file.
func FingerprintOfFile(path string) (string, error) {
	der, err := certificateDER(path)
	if err != nil {
		return "", err
	}

	return FingerprintOfDER(der), nil
}

// certificateDER returns the bytes of the certificate in the file.
func certificateDER(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the certificate %s: %w", path, err)
	}

	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s does not hold a certificate", path)
	}

	return block.Bytes, nil
}

// DefaultCertificateFiles returns the paths of the certificate and the key:
// they live next to the token file, in the user's configuration directory.
func DefaultCertificateFiles() (string, string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("finding the configuration directory: %w", err)
	}

	base := filepath.Join(dir, "cthrone")

	return filepath.Join(base, "cert.pem"), filepath.Join(base, "key.pem"), nil
}

// WriteCertificate stores the certificate and the key. The key is written
// for its owner only: it is the private half of the key pair.
func WriteCertificate(certPath, keyPath string, cert Certificate) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(certPath), err)
	}

	if err := os.WriteFile(certPath, cert.CertPEM, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", certPath, err)
	}

	if err := os.WriteFile(keyPath, cert.KeyPEM, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", keyPath, err)
	}

	return nil
}

// NormalizeFingerprint returns the fingerprint in the form it is compared
// in: lowercase hex, without separators. The form a user copies is often
// the one a tool prints with colons in it.
func NormalizeFingerprint(value string) string {
	replacer := strings.NewReplacer(":", "", " ", "", "\n", "", "\t", "")

	return strings.ToLower(replacer.Replace(value))
}

// ClientTLSConfig returns the configuration a client uses to talk to a host.
//
// The certificate is checked against the fingerprint, not against a
// certificate authority: the host's certificate is self-signed, so there is
// no authority to check it against. The normal verification is skipped on
// purpose — the fingerprint is the check, and running both would fail on a
// self-signed certificate before the fingerprint is ever looked at.
//
// An empty fingerprint with insecure set accepts any certificate. That is
// what "insecure" means, and it is why it has to be asked for.
func ClientTLSConfig(fingerprint string, insecure bool) (*tls.Config, error) {
	if insecure {
		return &tls.Config{InsecureSkipVerify: true}, nil //nolint:gosec // asked for by name
	}

	want := NormalizeFingerprint(fingerprint)
	if want == "" {
		return nil, errors.New("a fingerprint is needed to check the certificate, or --insecure to skip the check")
	}

	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // the fingerprint below is the check
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("the host presented no certificate")
			}

			got := FingerprintOfDER(rawCerts[0])
			if got != want {
				return fmt.Errorf("the certificate of the host does not match the fingerprint: expected %s, got %s", want, got)
			}

			return nil
		},
	}, nil
}
