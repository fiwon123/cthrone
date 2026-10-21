package natscore

import (
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
)

func TestSubjectFrom(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no argument uses the default",
			args: nil,
			want: DefaultSubject,
		},
		{
			name: "an empty argument uses the default",
			args: []string{""},
			want: DefaultSubject,
		},
		{
			name: "the argument is the subject",
			args: []string{"room-42"},
			want: "room-42",
		},
		{
			name: "only the first argument is the subject",
			args: []string{"room-42", "extra"},
			want: "room-42",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subjectFrom(tc.args); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBrokerURL(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		want       string
	}{
		{
			name:       "an empty value uses the default broker",
			configured: "",
			want:       DefaultBrokerURL,
		},
		{
			name:       "a configured broker is used",
			configured: "nats://broker.internal:4222",
			want:       "nats://broker.internal:4222",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := brokerURL(tc.configured); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// The broker credentials come from the environment, the way the NATS
// client expects them. They are not flags: a flag is visible in the
// process list.
func TestCredentialsComeFromTheEnvironment(t *testing.T) {
	t.Setenv(EnvUser, "cthrone")
	t.Setenv(EnvPassword, "a-secret")

	user, password := Credentials()
	if user != "cthrone" {
		t.Errorf("expected the user from the environment, got %q", user)
	}

	if password != "a-secret" {
		t.Errorf("expected the password from the environment, got %q", password)
	}
}

// A broker without credentials in the environment is not asked for any.
func TestCredentialsAreEmptyWithoutTheEnvironment(t *testing.T) {
	t.Setenv(EnvUser, "")
	t.Setenv(EnvPassword, "")

	user, password := Credentials()
	if user != "" || password != "" {
		t.Errorf("expected no credentials, got %q/%q", user, password)
	}
}

// A plain broker needs no certificate configuration, and demanding a
// fingerprint for one would refuse a connection that is perfectly fine.
func TestTLSConfigIsNotNeededForAPlainBroker(t *testing.T) {
	config, err := TLSConfig("nats://broker.internal:4222", "", false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if config != nil {
		t.Error("expected no TLS configuration for a plain broker")
	}
}

// The scheme of the URL says whether the connection is encrypted, the way
// a URL does.
func TestTLSConfigFollowsTheSchemeOfTheURL(t *testing.T) {
	if !usesTLS("tls://broker.internal:4222") {
		t.Error("expected a tls:// broker to use TLS")
	}

	if usesTLS("nats://broker.internal:4222") {
		t.Error("expected a nats:// broker not to use TLS")
	}

	// The scheme of a URL is case-insensitive, and the NATS client reads
	// it that way too, so the scheme is compared the same way here.
	if !usesTLS("TLS://broker.internal:4222") {
		t.Error("expected the scheme to be read whatever its case")
	}
}

// A client that is told to expect TLS and names neither a fingerprint nor
// --insecure is refused: connecting to whatever answers is not what was
// asked for.
func TestTLSConfigNeedsAFingerprintOrInsecure(t *testing.T) {
	_, err := TLSConfig("tls://broker.internal:4222", "", false)
	if err == nil {
		t.Fatal("expected the connection to be refused")
	}

	if !strings.Contains(err.Error(), "--cert-fingerprint") {
		t.Errorf("expected the error to name the flag, got %q", err.Error())
	}
}

// The certificate of the broker is checked against the fingerprint, the
// same way the websocket host's is.
func TestTLSConfigPinsTheCertificate(t *testing.T) {
	cert, err := auth.GenerateCertificate([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := TLSConfig("tls://broker.internal:4222", cert.Fingerprint, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.VerifyPeerCertificate([][]byte{cert.DER}, nil); err != nil {
		t.Errorf("expected the fingerprint to be matched, got %v", err)
	}
}
