package natscore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"

	"github.com/nats-io/nats.go"
)

// Host using subject string
func Host(subject string, app *app.Data) {
	ch := make(chan *nats.Conn)
	go natshandler.Connect(ch)

	conn := <-ch

	natshandler.SubscribeMessages(conn, subject)

	select {}
}
