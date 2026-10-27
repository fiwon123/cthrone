package natscore

import (
	"fmt"
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
	"github.com/fiwon123/cthrone/internal/session"
)

// OpenSession connects to the broker and returns a live session for a caller
// that is not the terminal — the GUI and the mobile app. The broker URL, the
// credentials and the TLS configuration are resolved exactly as Connect
// resolves them.
func OpenSession(args []string, app *app.Data, broker string, tlsConf auth.TLS) (session.Session, error) {
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

	return natshandler.NewSession(conn, subject)
}

// Connect using subject string and the broker to connect to
func Connect(args []string, app *app.Data, broker string, tlsConf auth.TLS) {
	subject := subjectFrom(args)

	url := brokerURL(broker)

	tlsConfig, err := TLSConfig(url, tlsConf.Fingerprint, tlsConf.Insecure)
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}

	user, password := Credentials()

	conn, err := natshandler.Connect(url, user, password, tlsConfig)
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer conn.Close()

	var msg string

	for {
		fmt.Print("you: ")
		fmt.Scanln(&msg)
		natshandler.PublishMessages(conn, msg, subject)
	}

}
