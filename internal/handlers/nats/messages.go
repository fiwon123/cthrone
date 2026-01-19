package natshandler

import (
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
)

// Publish Message using a string subject
func PublishMessages(nc *nats.Conn, message string, subject string) error {

	err := nc.Publish(subject, []byte(message))
	if err != nil {
		return fmt.Errorf("Error publishing: %v", err)
	}

	nc.Flush()

	return nil
}

// Subscribe Message using a string subject
func SubscribeMessages(nc *nats.Conn, subject string) {

	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		log.Printf("Received message: %s", string(m.Data))
	})

	if err != nil {
		log.Printf("Error subscribing: %v", err)
		return
	}
	defer sub.Unsubscribe()

	log.Println("Subscriptions active")

	select {}
}
