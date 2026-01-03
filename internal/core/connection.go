package core

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/handlers/chat"
)

func Connect(connectIP string, app *app.Data) {
	url := fmt.Sprintf("ws://%s:%d/ws", connectIP, app.Port)
	go chat.Connect(url)
}
