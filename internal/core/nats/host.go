package natscore

import (
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
)

// Host connects to the broker and runs the terminal chat loop.
func Host(args []string, app *app.Data, opts pairing.Options, broker string, tlsConf auth.TLS) {
	s, err := OpenSession(args, app, opts, broker, tlsConf)
	if err != nil {
		log.Fatal(err)
	}

	runTerminal(s)
}
