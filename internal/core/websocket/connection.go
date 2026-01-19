package websocketcore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

func Connect(args []string, app *app.Data) {
	connectIP := ""
	if len(args) > 0 {
		connectIP = args[0]
	} else {
		panic("ip to connect")
	}

	websockethandler.Connect(connectIP, app.Port)
}
