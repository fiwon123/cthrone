package natscore

import (
	"log"

	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
)

// Host using subject string and the broker to connect to
func Host(args []string, app *app.Data, broker string) {
	subject := subjectFrom(args)

	conn, err := natshandler.Connect(brokerURL(broker))
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer conn.Close()

	natshandler.SubscribeMessages(conn, subject)
}
