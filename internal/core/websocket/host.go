package websocketcore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

func Host(app *app.Data) {
	websockethandler.StartServer(app.Port)
}
