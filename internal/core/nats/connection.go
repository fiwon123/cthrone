package natscore

import (
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/chat"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
	"github.com/fiwon123/cthrone/internal/session"
)

// runTerminal is the loop a terminal session runs on. It is a variable so a
// test does not have to own a tty.
var runTerminal = chat.Run

// seal wraps a transport session and refuses to hand out one that is not
// sealed. A secret too short for the handshake closes the session here rather
// than letting the caller use it in the clear.
func seal(raw session.Session, secret, name string) (session.Session, error) {
	sealed, err := e2e.New(raw, e2e.Config{
		Secret: []byte(secret),
		Name:   name,
		Mode:   e2e.Bus,
	})
	if err != nil {
		raw.Close(err.Error())

		return nil, err
	}

	return sealed, nil
}

// OpenSession connects to the broker and returns a live session for a caller
// that is not the terminal — the GUI and the mobile app. The broker URL, the
// credentials, the shared secret and the TLS configuration are resolved
// exactly as Connect resolves them.
//
// The session is sealed with the shared secret before it is returned: a caller
// that has a NATS session has an encrypted one, or an error saying why not.
func OpenSession(args []string, app *app.Data, flagToken, tokenFile, broker string, tlsConf auth.TLS) (session.Session, error) {
	subject := subjectFrom(args)

	url := brokerURL(broker)

	tlsConfig, err := TLSConfig(url, tlsConf.Fingerprint, tlsConf.Insecure)
	if err != nil {
		return nil, err
	}

	user, password := Credentials()

	conn, err := natshandler.Connect(url, user, password, tlsConfig)
	if err != nil {
		return nil, err
	}

	raw, err := natshandler.NewSession(conn, subject)
	if err != nil {
		conn.Close()
		return nil, err
	}

	secret, err := sharedSecret(flagToken, tokenFile)
	if err != nil {
		raw.Close(err.Error())
		return nil, err
	}

	return seal(raw, secret, app.Name)
}

// Connect connects to the broker and runs the terminal chat loop until the peer
// disconnects or the user presses Ctrl-C.
func Connect(args []string, app *app.Data, flagToken, tokenFile string, broker string, tlsConf auth.TLS) {
	s, err := OpenSession(args, app, flagToken, tokenFile, broker, tlsConf)
	if err != nil {
		log.Fatal(err)
	}

	runTerminal(s)
}
