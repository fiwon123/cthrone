package natscore

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"

	"github.com/nats-io/nats.go"
)

// Connect using subject string
func Connect(args []string, app *app.Data) {
	subject := "chat"
	if len(args) > 0 {
		subject = args[0]
	}

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
