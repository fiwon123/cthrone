//go:build linux && !android

package bluetoothcore

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/fiwon123/cthrone/internal/chat"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	bluetoothhandler "github.com/fiwon123/cthrone/internal/handlers/bluetooth"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// listener is what a host needs from the GATT server: the next device, and a
// way to stop. It is an interface so a test can serve an in-memory one — no
// radio exists in a test, and the loop above this call is what is being
// checked.
type listener interface {
	Accept() (bluetoothhandler.Link, error)
	Close() error
}

// listen starts the GATT server. It is a variable for the same reason.
var listen = func(name string) (listener, error) { return bluetoothhandler.Listen(name) }

// runRoom is the console a host runs its room on: one stdin, every device in
// the room. It reports whether the user quit. It is a variable for the same
// reason as every other loop here — a test cannot press Ctrl-C on a tty.
var runRoom = chat.RunRoom

// HostHandle is a running Bluetooth host.
//
// It carries this device's fingerprint because the other device has to compare
// one with its own, and a GUI has nowhere to print it to.
type HostHandle struct {
	listener listener
	hub      *session.Hub

	// DeviceFingerprint is this device's key, in the device-key model. It is
	// empty when a shared secret is configured, which leaves nothing to
	// compare.
	DeviceFingerprint string

	stop      chan struct{}
	stopOnce  sync.Once
	accepting sync.WaitGroup
}

// Host serves a Bluetooth room and runs the terminal console over it, until the
// user quits or the process is stopped.
//
// One device at a time: the room exists so the console is the same as on the
// other transports, not because two devices can join. A device that leaves is
// noticed by the keepalive, and the slot is free for the next one.
func Host(app *app.Data, opts pairing.Options, confirm ...Confirm) error {
	h, err := OpenServer(app, opts, func(session.Session) {}, confirm...)
	if err != nil {
		return err
	}
	defer h.Stop()

	// The instructions belong to the terminal console, not to OpenServer: a
	// GUI host has no terminal to print them to, and the window says the same
	// things on its own screen.
	fmt.Println("Bluetooth: serving as", nameFor(app), "— one device at a time")
	fmt.Println("The other device runs: cthrone connect --bluetooth")
	fmt.Println("Text is what this link is for: a file chunk is thousands of 20-byte packets.")

	go func() {
		if runRoom(h.Hub()) {
			_ = h.Stop()
		}
	}()

	_ = h.Wait()

	return nil
}

// OpenServer starts a host and returns a handle for a caller that is not the
// terminal — the GUI. Every device that is let in arrives at onSession already
// sealed, and only after it has proved its key: a stranger that connects to the
// radio but never authenticates reaches neither the hub nor the consumer.
//
// confirm is the gate a first contact is put to. The Bluetooth host asks the
// same question the websocket host asks, because it has the same thing to
// compare: two screens and a six-digit code.
func OpenServer(app *app.Data, opts pairing.Options, onSession func(session.Session), confirm ...Confirm) (*HostHandle, error) {
	cred, err := pairing.Load(opts)
	if err != nil {
		return nil, err
	}

	name := nameFor(app)

	listener, err := listen(name)
	if err != nil {
		return nil, err
	}

	gate := func() Confirm {
		if len(confirm) == 0 {
			return nil
		}

		return confirm[0]
	}()

	h := &HostHandle{
		listener:          listener,
		hub:               session.NewHub(),
		DeviceFingerprint: cred.Fingerprint(),
		stop:              make(chan struct{}),
	}

	h.accepting.Add(1)

	go h.serve(cred, name, gate, onSession)

	return h, nil
}

// nameFor is the name the host advertises: the device name when one was given,
// and "cthrone" otherwise — a scan has to have something to show.
func nameFor(app *app.Data) string {
	if app.Name != "" {
		return app.Name
	}

	return "cthrone"
}

// Hub is the room the host relays to.
func (h *HostHandle) Hub() *session.Hub { return h.hub }

// Wait blocks until the host has stopped.
func (h *HostHandle) Wait() error {
	if h.stop == nil {
		return nil
	}

	<-h.stop

	h.accepting.Wait()

	return nil
}

// Stop ends the host: the advertisement, the current device, and the wait for
// the next one. It is safe to call more than once, and safe on a handle that
// never started: a zero value is a stopped host, not a panic.
func (h *HostHandle) Stop() error {
	h.stopOnce.Do(func() {
		if h.stop != nil {
			close(h.stop)
		}

		if h.listener != nil {
			_ = h.listener.Close()
		}
	})

	return nil
}

// serve takes one device at a time, seals what it says, and puts it in the room
// once it has proved its key.
func (h *HostHandle) serve(cred pairing.Credential, name string, gate Confirm, onSession func(session.Session)) {
	defer h.accepting.Done()

	for {
		link, err := h.listener.Accept()
		if err != nil {
			select {
			case <-h.stop:
				return
			default:
			}

			// A wait that timed out is not a failure: the host keeps waiting
			// for a device, which is what hosting is.
			if errors.Is(err, bluetoothhandler.ErrNoDevice) {
				continue
			}

			log.Println("Bluetooth:", err)

			return
		}

		// The keepalive is what turns "the radio went quiet" into a closed
		// session, so the slot is free for the next device instead of being
		// held by one that walked away.
		raw := bluetoothhandler.New(bluetoothhandler.WithKeepalive(link))

		sealed, err := seal(raw, cred, name, gate)
		if err != nil {
			log.Println("Bluetooth: refused a device —", err)

			continue
		}

		done := make(chan struct{})

		var once sync.Once

		sealed.AddCloseHandler(func(string) { once.Do(func() { close(done) }) })

		admit := func() {
			h.hub.Register(sealed)
			onSession(sealed)
		}

		// The consumer starts on the first peer that proves its identity, not
		// on the radio connection: a device that connects and says nothing
		// takes over neither the terminal nor the room.
		if conn, ok := sealed.(*e2e.Conn); ok {
			var joined sync.Once

			conn.OnPeer(func(e2e.Peer) { joined.Do(admit) })
		} else {
			admit()
		}

		select {
		case <-done:
			// The device left. Accept waits out the quiet gap and takes the
			// next one.
		case <-h.stop:
			sealed.Close("the host stopped")

			return
		}
	}
}
