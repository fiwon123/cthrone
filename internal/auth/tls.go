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

// TLS says how a transport is encrypted.
//
// It is shared by the transports: the websocket one and the NATS one are
// both connections to a peer, and both are checked the same way — against
// the fingerprint of the peer's certificate, because a peer on a LAN has
// no certificate authority.
type TLS struct {
	// Enabled makes the transport encrypted. The websocket host serves
	// https and the websocket client expects it; both are the default, and
	// --no-tls turns them off. The NATS transport decides from the scheme of
	// the broker URL instead, the way a URL does.
	Enabled bool

	// CertFile and KeyFile are where the host keeps its certificate. They
	// are empty by default, which means the files in the user's
	// configuration directory.
	CertFile string
	KeyFile  string

	// Fingerprint is the certificate the client pins by hand. It is checked
	// instead of a certificate authority: the host's certificate is
	// self-signed, so there is no authority to check it against. An empty
	// fingerprint means the certificate is pinned on first contact.
	Fingerprint string

	// Insecure makes the client accept any certificate. It is what the
	// flag says, and it is why it has to be asked for.
	Insecure bool
}

// CertPins is the record of the host certificates this device trusts on first
// contact. The websocket client keeps it beside the device pins, in
// peers.json, because both answer the same question: is this the peer I met
// last time? discovery.PinStore is the implementation.
type CertPins interface {
	// CheckCert reports whether the certificate presented at address is the
	// one pinned there, and reports first when nothing is pinned yet.
	CheckCert(address, fingerprint string) (first bool, err error)

	// PinCert records the certificate presented at address.
	PinCert(address, fingerprint string) error
}

// TransportTLS resolves the pair of switches into the setting the websocket
// transport uses. TLS is the default, so a run that passes neither is still
// encrypted: --no-tls is the opt-out, and --tls is kept only so a v0.6.0
// command line still runs. Asking for both is a contradiction, not a
// preference.
func TransportTLS(tlsFlag, noTLSFlag bool) (bool, error) {
	if tlsFlag && noTLSFlag {
		return false, errors.New("--tls and --no-tls cannot both be given: TLS is the default, --no-tls turns it off")
	}

	return !noTLSFlag, nil
}

// TLSConfig returns the configuration a client uses to check the certificate
// of its peer, or nil when the transport is not encrypted.
//
// A fingerprint pins the certificate by hand, and insecure accepts any of
// them. With neither, the certificate is trusted on first use: the one the
// host presents is recorded in pins under address, and a different one at the
// same address later is refused. That is what lets TLS be the default with no
// value to copy from the other device.
func TLSConfig(tlsConf TLS, address string, pins CertPins) (*tls.Config, error) {
	if !tlsConf.Enabled {
		return nil, nil
	}

	if tlsConf.Fingerprint != "" {
		return ClientTLSConfig(tlsConf.Fingerprint, false)
	}

	if tlsConf.Insecure {
		return ClientTLSConfig("", true)
	}

	return trustOnFirstUse(address, pins)
}

// trustOnFirstUse returns the configuration that records the certificate the
// host presents, and refuses a different one under the same address.
//
// The first contact is the only moment a machine in the middle can insert
// itself: the pin is written from what arrives, so the address has to name the
// host the user meant. The device key is the stronger gate behind it — the
// session is refused unless the peer proves the key it was pinned by.
func trustOnFirstUse(address string, pins CertPins) (*tls.Config, error) {
	if pins == nil {
		return nil, errors.New("the certificate of the host is pinned on first contact, and there is nowhere to record it: give --cert-fingerprint or --insecure")
	}

	if strings.TrimSpace(address) == "" {
		return nil, errors.New("the certificate of the host is pinned by address, and the address is empty")
	}

	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // the pin below is the check
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("the host presented no certificate")
			}

			got := FingerprintOfDER(rawCerts[0])

			if _, err := pins.CheckCert(address, got); err != nil {
				return err
			}

			// Recording refreshes "last seen" after the first contact, the
			// way a device pin does: the listing answers when this host was
			// last reached, not only when it was first trusted.
			if err := pins.PinCert(address, got); err != nil {
				return fmt.Errorf("recording the certificate of %s: %w", address, err)
			}

			return nil
		},
	}, nil
}

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
