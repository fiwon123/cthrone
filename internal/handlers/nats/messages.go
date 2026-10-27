package natshandler

import (
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
)

type publisher interface {
	Publish(string, []byte) error
	Flush() error
}

type subscription interface {
	Unsubscribe() error
}

type subscriber interface {
	Subscribe(string, nats.MsgHandler) (subscription, error)
}

type natsSubscriber struct {
	nc *nats.Conn
}

func (n *natsSubscriber) Subscribe(subj string, h nats.MsgHandler) (subscription, error) {
	return n.nc.Subscribe(subj, h)
}

var (
	pub  = func(nc *nats.Conn) publisher { return nc }
	subs = func(nc *nats.Conn) subscriber { return &natsSubscriber{nc: nc} }
)

// Publish Message using a string subject
func PublishMessages(nc *nats.Conn, message string, subject string) error {
	return publishMessages(pub(nc), message, subject)
}

func publishMessages(p publisher, message string, subject string) error {
	err := p.Publish(subject, []byte(message))
	if err != nil {
		return fmt.Errorf("Error publishing: %v", err)
	}

	if err := p.Flush(); err != nil {
		return err
	}

	return nil
}

// Subscribe Message using a string subject, and block until the connection
// closes. It is the terminal consumer: the GUI uses NewSession instead.
func SubscribeMessages(nc *nats.Conn, subject string) {
	if _, err := subscribeMessages(subs(nc), subject); err != nil {
		log.Printf("Error subscribing: %v", err)

		return
	}

	select {}
}

// subscribeMessages registers the handler and returns the subscription. It
// does not block and it does not unsubscribe: the caller owns the
// subscription for as long as it needs it.
func subscribeMessages(s subscriber, subject string) (subscription, error) {
	sub, err := s.Subscribe(subject, func(m *nats.Msg) {
		log.Printf("Received message: %s", string(m.Data))
	})

	if err != nil {
		log.Printf("Error subscribing: %v", err)

		return nil, err
	}

	log.Println("Subscriptions active")

	return sub, nil
}
