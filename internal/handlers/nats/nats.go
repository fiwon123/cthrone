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

	log.Println("Connected to NATS server")

	ch <- nc

	select {}
}

func PublishMessages(nc *nats.Conn) {

	err := nc.Publish("greeting", []byte("Hello NATS!"))
	if err != nil {
		log.Printf("Error publishing: %v", err)
		return
	}

	nc.Flush()

	log.Println("Messages published successfully")

}

func SubscribeToMessages(nc *nats.Conn) {

	sub, err := nc.Subscribe("greeting", func(m *nats.Msg) {
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
