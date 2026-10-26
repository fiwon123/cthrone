package natshandler

import (
	"crypto/tls"

	"github.com/nats-io/nats.go"
)

// dial is the function that connects to a broker. It is a variable so a
// test can replace it: the connection is the one step that needs a real
// broker, and a test that cannot reach it cannot check that the configured
// URL is the one being used.
var dial = nats.Connect

// Connect to NATS server.
//
// The credentials and the TLS configuration are options, not part of the
// URL: a URL with a password in it is visible in the process list, and a
// URL is also what gets written down in a log.
func Connect(url string, user, password string, tlsConfig *tls.Config) (*nats.Conn, error) {
	options := []nats.Option{}
	if user != "" {
		options = append(options, nats.UserInfo(user, password))
	}

	if tlsConfig != nil {
		options = append(options, nats.Secure(tlsConfig))
	}

	return dial(url, options...)
}
