package websocketcore

import (
	"errors"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/chat"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/session"
)

// connect is the function that connects to the host. It is a variable so a
// test can replace it: the connection needs a host to talk to, and a test
// that cannot reach one cannot check what the command does with the error.
var connect = websockethandler.Connect

// runTerminal is the loop a terminal session runs on. It is a variable so a
// test does not have to own a tty.
var runTerminal = chat.Run

// Connect connects to a host and runs the terminal chat loop until the peer
// disconnects or the user presses Ctrl-C.
func Connect(args []string, app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) error {
	s, err := open(args, app, flagToken, tokenFile, tlsConf)
	if err != nil {
		return err
	}
	defer s.Close("goodbye")

	runTerminal(s)

	return nil
}

// Dial connects to a host and returns a live session for a caller that is not
// the terminal — the GUI and the mobile app. The token and the TLS
// configuration are resolved the same way Connect resolves them, so both
// paths refuse a connection for the same reasons.
func Dial(args []string, app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) (session.Session, error) {
	return open(args, app, flagToken, tokenFile, tlsConf)
}

// open dials the host and seals the session. Every path through this package
// ends here, so there is no way to get a websocket session that is not
// encrypted.
func open(args []string, app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) (session.Session, error) {
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

	raw, err := connect(address, app.Port, token, tlsConf.Enabled, tlsConfig)
	if err != nil {
		return nil, err
	}

	return seal(raw, token, app.Name)
}

// seal wraps a transport session and refuses to hand out one that is not
// sealed. A secret too short for the handshake closes the session here rather
// than letting the caller use it in the clear.
func seal(raw session.Session, secret, name string) (session.Session, error) {
	sealed, err := e2e.New(raw, e2e.Config{
		Secret: []byte(secret),
		Name:   name,
		Mode:   e2e.Single,
	})
	if err != nil {
		raw.Close(err.Error())

		return nil, err
	}

	return sealed, nil
}

// connectAddress returns the address of the host from the argument, or an
// empty string when there is none.
func connectAddress(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
