//go:build linux && !android

package bluetoothcore

import (
	"github.com/fiwon123/cthrone/internal/chat"
	"github.com/fiwon123/cthrone/internal/data/app"
	bluetoothhandler "github.com/fiwon123/cthrone/internal/handlers/bluetooth"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// dial finds the host and returns the link. It is a variable so a test can
// supply an in-memory one: no radio exists in a test, and what is being checked
// is the sealing above it.
var dial = bluetoothhandler.Dial

// runTerminal is the loop a terminal session runs on.
var runTerminal = chat.Run

// Connect finds a Bluetooth host, seals a session to it, and runs the terminal
// chat loop until the device leaves or the user presses Ctrl-C.
func Connect(args []string, app *app.Data, opts pairing.Options) error {
	s, err := OpenSession(args, app, opts)
	if err != nil {
		return err
	}
	defer s.Close("goodbye")

	runTerminal(s)

	return nil
}

// OpenSession connects to a Bluetooth host and returns a live sealed session
// for a caller that is not the terminal — the GUI.
func OpenSession(args []string, app *app.Data, opts pairing.Options) (session.Session, error) {
	cred, err := pairing.Load(opts)
	if err != nil {
		return nil, err
	}

	link, err := dial(target(args), dialTimeout)
	if err != nil {
		return nil, err
	}

	return seal(bluetoothhandler.New(bluetoothhandler.WithKeepalive(link)), cred, app.Name, nil)
}

// target is what the user named to connect to: a Bluetooth address, a device
// name, or nothing at all for the first cthrone device in range.
func target(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}
