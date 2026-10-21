package websocketcore

import (
	"errors"

	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

// connect is the function that connects to the host. It is a variable so a
// test can replace it: the connection needs a host to talk to, and a test
// that cannot reach one cannot check what the command does with the error.
var connect = websockethandler.Connect

// Connect receive a args with ip for establish connection
func Connect(args []string, app *app.Data, flagToken, tokenFile string, tlsConf TLS) error {
	address := connectAddress(args)
	if address == "" {
		return errors.New("the address of the host to connect to is required")
	}

	token, err := clientToken(tokenConfig{flagValue: flagToken, filePath: tokenFile})
	if err != nil {
		return err
	}

	tlsConfig, err := clientTLSConfig(tlsConf)
	if err != nil {
		return err
	}

	return connect(address, app.Port, token, tlsConf.Enabled, tlsConfig)
}

// connectAddress returns the address of the host from the argument, or an
// empty string when there is none.
func connectAddress(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
