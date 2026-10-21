package natscore

import (
	"fmt"
	"log"

	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
)

// Connect using subject string and the broker to connect to
func Connect(args []string, app *app.Data, broker string) {
	subject := subjectFrom(args)

	conn, err := natshandler.Connect(brokerURL(broker))
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer conn.Close()

	var msg string

	for {
		fmt.Print("you: ")
		fmt.Scanln(&msg)
		natshandler.PublishMessages(conn, msg, subject)
	}

}
