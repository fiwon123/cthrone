package websocketcore

import (
	"errors"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/session"
)

// connect is the function that connects to the host. It is a variable so a
// test can replace it: the connection needs a host to talk to, and a test
// that cannot reach one cannot check what the command does with the error.
var connect = websockethandler.Connect

// Connect receive a args with ip for establish connection
func Connect(args []string, app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) error {
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

// Dial connects to a host and returns a live session for a caller that is not
// the terminal — the GUI and the mobile app. The token and the TLS
// configuration are resolved the same way Connect resolves them, so both
// paths refuse a connection for the same reasons.
func Dial(args []string, app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) (session.Session, error) {
	address := connectAddress(args)
	if address == "" {
		return nil, errors.New("the address of the host to connect to is required")
	}

	token, err := clientToken(tokenConfig{flagValue: flagToken, filePath: tokenFile})
	if err != nil {
		return nil, err
	}

	tlsConfig, err := clientTLSConfig(tlsConf)
	if err != nil {
		return nil, err
	}

	return websockethandler.Dial(address, app.Port, token, tlsConf.Enabled, tlsConfig)
}

// connectAddress returns the address of the host from the argument, or an
// empty string when there is none.
func connectAddress(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
