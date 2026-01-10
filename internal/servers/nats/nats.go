package nats

import (
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

func Connect() {

	// Connect to NATS server
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer nc.Close()

	log.Println("Connected to NATS server")
}

func publishMessages(nc *nats.Conn) {
	// Simple publish
	err := nc.Publish("greeting", []byte("Hello NATS!"))
	if err != nil {
		log.Printf("Error publishing: %v", err)
		return
	}

	// Publish with reply subject
	err = nc.PublishRequest("service.request", "service.reply", []byte("Request data"))
	if err != nil {
		log.Printf("Error publishing request: %v", err)
		return
	}

	// Ensure message is sent
	nc.Flush()

	log.Println("Messages published successfully")
}

func subscribeToMessages(nc *nats.Conn) {
	// Simple subscription
	sub, err := nc.Subscribe("greeting", func(m *nats.Msg) {
		log.Printf("Received message: %s", string(m.Data))
	})
	if err != nil {
		log.Printf("Error subscribing: %v", err)
		return
	}
	defer sub.Unsubscribe()

	// Subscription with queue group (load balancing)
	queueSub, err := nc.QueueSubscribe("work.queue", "workers", func(m *nats.Msg) {
		log.Printf("Worker processing: %s", string(m.Data))
		// Simulate work
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		log.Printf("Error creating queue subscription: %v", err)
		return
	}
	defer queueSub.Unsubscribe()

	log.Println("Subscriptions active")
}
