package websocketcore

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

func Connect(args []string, app *app.Data) {
	connectIP := ""
	if len(args) > 0 {
		connectIP = args[0]
	}

	url := fmt.Sprintf("ws://%s:%d/ws", connectIP, app.Port)
	go websockethandler.Connect(url)

	select {}
}
