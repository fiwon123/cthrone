package auth

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/discovery"
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

// A client that is told to use TLS and names neither a fingerprint nor
// --insecure trusts the certificate on first use: the one that arrives is
// recorded, and the next run asks for the same one.
func TestTLSConfigTrustsTheFirstCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	store, err := discovery.NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	config, err := TLSConfig(TLS{Enabled: true}, "192.168.1.5:8080", store)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Fatalf("the first certificate was refused: %v", err)
	}

	pinned := store.Certificates()
	if len(pinned) != 1 {
		t.Fatalf("the first contact recorded %d certificates, want 1", len(pinned))
	}

	if pinned[0].Address != "192.168.1.5:8080" || pinned[0].Fingerprint != cert.Fingerprint {
		t.Errorf("pinned %+v, want %q with %q", pinned[0], "192.168.1.5:8080", cert.Fingerprint)
	}
}

// The second contact has to present the pinned certificate. A different one
// at the same address is refused, and the error names the way back.
func TestTLSConfigRefusesAChangedCertificate(t *testing.T) {
	first, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	other, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	store, err := discovery.NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	config, err := TLSConfig(TLS{Enabled: true}, "192.168.1.5:8080", store)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{first.DER}, nil); err != nil {
		t.Fatalf("the first certificate was refused: %v", err)
	}

	err = config.VerifyPeerCertificate([][]byte{other.DER}, nil)
	if !errors.Is(err, discovery.ErrCertificateChanged) {
		t.Fatalf("a changed certificate = %v, want ErrCertificateChanged", err)
	}

	if !strings.Contains(err.Error(), "192.168.1.5:8080") {
		t.Errorf("the refusal does not name the address: %v", err)
	}
}

// countingPins is a pin store that answers with a fixed verdict and counts
// what the TLS configuration asked of it, so a test can see the difference
// between "checked" and "recorded".
type countingPins struct {
	first    bool
	checkErr error
	checked  int
	pinned   int
}

func (p *countingPins) CheckCert(string, string) (bool, error) {
	p.checked++

	return p.first, p.checkErr
}

func (p *countingPins) PinCert(string, string) error {
	p.pinned++

	return nil
}

// Every contact records the certificate, not only the first: recording is
// what refreshes "last seen", and a listing that never moves would claim the
// host was reached only on the day it was first trusted. A certificate the
// check refuses is never recorded.
func TestTLSConfigRefreshesTheLastSeenOfAPinnedCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a known certificate is recorded again", func(t *testing.T) {
		pins := &countingPins{first: false}

		config, err := TLSConfig(TLS{Enabled: true}, "192.168.1.5:8080", pins)
		if err != nil {
			t.Fatal(err)
		}

		if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
			t.Fatalf("the pinned certificate was refused: %v", err)
		}

		if pins.checked != 1 || pins.pinned != 1 {
			t.Errorf("checked %d times and recorded %d, want one of each", pins.checked, pins.pinned)
		}
	})

	t.Run("a refused certificate is not recorded", func(t *testing.T) {
		pins := &countingPins{checkErr: discovery.ErrCertificateChanged}

		config, err := TLSConfig(TLS{Enabled: true}, "192.168.1.5:8080", pins)
		if err != nil {
			t.Fatal(err)
		}

		if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); !errors.Is(err, discovery.ErrCertificateChanged) {
			t.Fatalf("a changed certificate = %v, want ErrCertificateChanged", err)
		}

		if pins.pinned != 0 {
			t.Errorf("a refused certificate was recorded %d times, want 0", pins.pinned)
		}
	})
}

// A fingerprint given by hand is the check, and it is not written to the pin
// store: the user supplies it, and a pin recorded behind their back would
// outlive the flag they typed it with.
func TestTLSConfigPinsByHandWithoutRecording(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	store, err := discovery.NewPinStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}

	config, err := TLSConfig(TLS{Enabled: true, Fingerprint: cert.Fingerprint}, "192.168.1.5:8080", store)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Errorf("the pinned certificate was refused: %v", err)
	}

	if pinned := store.Certificates(); len(pinned) != 0 {
		t.Errorf("a hand-pinned certificate was recorded: %+v", pinned)
	}
}

// Without a fingerprint, --insecure, or pins there is nowhere to record the
// first certificate, so the client is refused rather than left accepting
// whatever answers.
func TestTLSConfigNeedsAPinStoreWithoutAFingerprint(t *testing.T) {
	_, err := TLSConfig(TLS{Enabled: true}, "192.168.1.5:8080", nil)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	for _, flag := range []string{"--cert-fingerprint", "--insecure"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("expected the error to name %s, got %q", flag, err.Error())
		}
	}
}

// A transport that is not encrypted needs no configuration at all.
func TestTLSConfigIsNilWithoutTLS(t *testing.T) {
	config, err := TLSConfig(TLS{}, "192.168.1.5:8080", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config != nil {
		t.Error("expected no TLS configuration when TLS is off")
	}
}

// The pair of switches resolves to the default and to the opt-out. Asking
// for both is a contradiction, not a preference.
func TestTransportTLS(t *testing.T) {
	tests := []struct {
		name    string
		tlsFlag bool
		noTLS   bool
		want    bool
		wantErr bool
	}{
		{name: "nothing passed: TLS is the default", want: true},
		{name: "--tls is still accepted", tlsFlag: true, want: true},
		{name: "--no-tls is the opt-out", noTLS: true, want: false},
		{name: "both contradict each other", tlsFlag: true, noTLS: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TransportTLS(tt.tlsFlag, tt.noTLS)
			if (err != nil) != tt.wantErr {
				t.Fatalf("TransportTLS(%v, %v) error = %v, wantErr %v", tt.tlsFlag, tt.noTLS, err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("TransportTLS(%v, %v) = %v, want %v", tt.tlsFlag, tt.noTLS, got, tt.want)
			}
		})
	}
}

// TLSFromFlags is where the command line becomes the transport's setting: the
// host and the client both call it, so "no flag means encrypted" cannot come
// out differently on the two ends. The fingerprint and --insecure travel with
// it unchanged; TLSConfig decides what they mean.
func TestTLSFromFlags(t *testing.T) {
	tests := []struct {
		name        string
		tlsFlag     bool
		noTLS       bool
		fingerprint string
		insecure    bool
		want        TLS
		wantErr     bool
	}{
		{
			name: "nothing passed: encrypted by default",
			want: TLS{Enabled: true},
		},
		{
			name:        "the opt-out and the values it carries",
			noTLS:       true,
			fingerprint: "ab",
			insecure:    true,
			want:        TLS{Enabled: false, Fingerprint: "ab", Insecure: true},
		},
		{
			name:    "--tls is still accepted",
			tlsFlag: true,
			want:    TLS{Enabled: true},
		},
		{
			name:    "both switches contradict each other",
			tlsFlag: true,
			noTLS:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TLSFromFlags(tt.tlsFlag, tt.noTLS, tt.fingerprint, tt.insecure)
			if (err != nil) != tt.wantErr {
				t.Fatalf("TLSFromFlags error = %v, wantErr %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("TLSFromFlags = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// --insecure is the flag that asks for no check at all, so it wins over a
// fingerprint that also arrived — from --cert-fingerprint or from a setting the
// GUI kept. Checking the fingerprint first would make "accept any certificate"
// refuse a host the user asked not to check.
func TestTLSConfigInsecureWinsOverTheFingerprint(t *testing.T) {
	config, err := TLSConfig(TLS{Enabled: true, Fingerprint: strings.Repeat("00", 32), Insecure: true}, "192.168.1.5:8080", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Nothing checks the certificate: no fingerprint hook, and the standard
	// verification skipped as well.
	if config.VerifyPeerCertificate != nil {
		t.Error("--insecure built a certificate check anyway")
	}

	if !config.InsecureSkipVerify {
		t.Error("--insecure did not skip the standard verification")
	}
}

// The pinned certificate is the one the transport asks for.
func TestTLSConfigPinsTheCertificate(t *testing.T) {
	cert, err := GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := TLSConfig(TLS{Enabled: true, Fingerprint: cert.Fingerprint}, "192.168.1.5:8080", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Errorf("expected the fingerprint to be matched, got %v", err)
	}
}
