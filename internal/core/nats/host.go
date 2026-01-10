package natscore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
	"github.com/nats-io/nats.go"
)

func Host(app *app.Data) {
	ch := make(chan *nats.Conn)
	go natshandler.Connect(ch)

	natshandler.SubscribeToMessages(<-ch)

	select {}
}
