package natscore

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"

	"github.com/nats-io/nats.go"
)

func Connect(subject string, app *app.Data) {
	ch := make(chan *nats.Conn)
	go natshandler.Connect(ch)

	conn := <-ch

	var msg string

	for {
		fmt.Print("you: ")
		fmt.Scanln(&msg)
		natshandler.PublishMessages(conn, msg, subject)
	}

}
