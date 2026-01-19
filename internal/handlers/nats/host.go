package natshandler

import (
	"log"

	"github.com/nats-io/nats.go"
)

func Connect(ch chan *nats.Conn) {

	// Connect to NATS server
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer nc.Close()

	ch <- nc

	select {}
}
