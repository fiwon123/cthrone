package natscore

import (
	"github.com/nats-io/nats.go"
)

// DefaultBrokerURL is the broker the NATS transport connects to when
// nothing else is configured.
const DefaultBrokerURL = nats.DefaultURL

// DefaultSubject is the subject the host subscribes to and the connect
// side publishes to, when no subject is given.
const DefaultSubject = "chat"

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
