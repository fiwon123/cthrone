package natscore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"

	"github.com/nats-io/nats.go"
)

// Host using subject string
func Host(args []string, app *app.Data) {
	subject := "chat"
	if len(args) > 0 {
		subject = args[0]
	}

	ch := make(chan *nats.Conn)
	go natshandler.Connect(ch)

	conn := <-ch

	natshandler.SubscribeMessages(conn, subject)

	select {}
}
