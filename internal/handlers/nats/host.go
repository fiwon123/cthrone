package natshandler

import (
	"log"

	"github.com/nats-io/nats.go"
)

// Connect to NATS server
func Connect(ch chan *nats.Conn) {

	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer nc.Close()

	ch <- nc

	select {}
}
