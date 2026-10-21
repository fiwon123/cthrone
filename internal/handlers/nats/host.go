package natshandler

import (
	"github.com/nats-io/nats.go"
)

// dial is the function that connects to a broker. It is a variable so a
// test can replace it: the connection is the one step that needs a real
// broker, and a test that cannot reach it cannot check that the configured
// URL is the one being used.
var dial = nats.Connect

// Connect to NATS server
func Connect(url string) (*nats.Conn, error) {
	return dial(url)
}
