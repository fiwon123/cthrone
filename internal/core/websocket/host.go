package websocketcore

import (
	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

// host is the function that serves the chat endpoint. It is a variable so a
// test can replace it: serving means listening on a port, and a test cannot
// do that.
var host = websockethandler.Host

// Host open connection
func Host(app *app.Data, flagToken, tokenFile string) error {
	token := ""
	if !app.AllowAnonymous {
		var err error

		token, err = hostToken(tokenConfig{flagValue: flagToken, filePath: tokenFile})
		if err != nil {
			return err
		}
	}

	return host(app.Port, app.Host, token, app.AllowAnonymous)
}
