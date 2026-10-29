package natscore

import (
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
)

// Host connects to the broker and runs the terminal chat loop.
func Host(args []string, app *app.Data, flagToken, tokenFile string, broker string, tlsConf auth.TLS) {
	s, err := OpenSession(args, app, flagToken, tokenFile, broker, tlsConf)
	if err != nil {
		log.Fatal(err)
	}

	runTerminal(s)
}
