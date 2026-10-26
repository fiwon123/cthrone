package natscore

import (
	"crypto/tls"
	"errors"
	"os"
	"strings"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/nats-io/nats.go"
)

// DefaultBrokerURL is the broker the NATS transport connects to when
// nothing else is configured.
const DefaultBrokerURL = nats.DefaultURL

// DefaultSubject is the subject the host subscribes to and the connect
// side publishes to, when no subject is given.
const DefaultSubject = "chat"

// The broker credentials come from the environment, the way the NATS
// client expects them. They are not flags: a flag is visible in the
// process list of every machine the command runs on.
const (
	EnvUser     = "NATS_USER"
	EnvPassword = "NATS_PASSWORD"
)

// Credentials returns the broker credentials from the environment. An
// empty user means the broker is not asked for any: a broker without
// authorization is a dev broker, and refusing to talk to it would be
// wrong.
func Credentials() (string, string) {
	return os.Getenv(EnvUser), os.Getenv(EnvPassword)
}

// usesTLS reports whether the broker URL asks for an encrypted
// connection. The scheme says it, the way a URL does.
func usesTLS(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "tls://")
}

// TLSConfig returns the configuration used to check the broker's
// certificate.
//
// It is nil unless the URL asks for TLS: a plain broker needs no
// configuration, and demanding a fingerprint for one would refuse a
// connection that is perfectly fine.
func TLSConfig(url string, fingerprint string, insecure bool) (*tls.Config, error) {
	if !usesTLS(url) {
		return nil, nil
	}

	if fingerprint == "" && !insecure {
		return nil, errors.New("a tls:// broker needs --cert-fingerprint to check the certificate, or --insecure to skip the check")
	}

	return auth.ClientTLSConfig(fingerprint, insecure)
}

// subjectFrom returns the subject from the positional argument, or the
// default when there is none.
func subjectFrom(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}

	return DefaultSubject
}

// brokerURL returns the broker to connect to, falling back to the default
// when the configured value is empty.
func brokerURL(configured string) string {
	if configured == "" {
		return DefaultBrokerURL
	}

	return configured
}
