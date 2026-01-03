package core

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/servers/ws"
)

func Host(app *app.Data) {

	go ws.StartServer(app.Port)

	select {}
}
