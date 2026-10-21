package natscore

import (
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	natshandler "github.com/fiwon123/cthrone/internal/handlers/nats"
)

// Host using subject string and the broker to connect to
func Host(args []string, app *app.Data, broker string, tlsConf auth.TLS) {
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

	natshandler.SubscribeMessages(conn, subject)
}
