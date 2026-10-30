package websocketcore

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/chat"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/pairing"
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
func Connect(args []string, app *app.Data, opts pairing.Options, tlsConf auth.TLS) error {
	s, err := open(args, app, opts, tlsConf)
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
func Dial(args []string, app *app.Data, opts pairing.Options, tlsConf auth.TLS) (session.Session, error) {
	return open(args, app, opts, tlsConf)
}

// open dials the host and seals the session. Every path through this package
// ends here, so there is no way to get a websocket session that is not
// encrypted.
func open(args []string, app *app.Data, opts pairing.Options, tlsConf auth.TLS) (session.Session, error) {
	address := connectAddress(args)
	if address == "" {
		return nil, errors.New("the address of the host to connect to is required")
	}

	cred, err := pairing.Load(opts)
	if err != nil {
		return nil, err
	}

	// A nil store is passed as a nil interface: a typed nil would be non-nil
	// to the TLS configuration, which would then try to record a pin in it.
	var pins auth.CertPins
	if store := cred.Pins(); store != nil {
		pins = store
	}

	tlsConfig, err := clientTLSConfig(tlsConf, pinAddress(address, app.Port), pins)
	if err != nil {
		return nil, err
	}

	raw, err := connect(address, app.Port, cred.Secret(), tlsConf.Enabled, tlsConfig)
	if err != nil {
		return nil, err
	}

	return seal(raw, cred, app.Name)
}

// seal wraps a transport session and refuses to hand out one that is not
// sealed. A credential the handshake cannot use closes the session here rather
// than letting the caller use it in the clear.
//
// confirm is the gate a host answers first contacts with. A client passes none:
// the code it shows is on the other screen, and the side that does not ask is
// the side that waits.
func seal(raw session.Session, cred pairing.Credential, name string, confirm ...Confirm) (session.Session, error) {
	cfg := cred.Config(e2e.Single, name)

	if len(confirm) > 0 {
		cfg.Confirm = confirm[0]
	}

	sealed, err := e2e.New(raw, cfg)
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

// pinAddress is the key a host's certificate is pinned under: the address the
// client dialed, with the port the dial actually uses. It is the same form
// that goes into the URL, so the pin names the host the user asked for rather
// than a second spelling of it.
func pinAddress(address string, port int) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return strings.ToLower(address)
	}

	return strings.ToLower(fmt.Sprintf("%s:%d", address, port))
}
