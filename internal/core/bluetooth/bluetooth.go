// Package bluetoothcore is the Bluetooth transport: the operation, above the
// GATT session that carries the bytes.
//
// It is the same shape as the other two transports — a host that serves a room,
// a client that connects and runs the terminal loop — and it differs in what it
// can promise, because a BLE peripheral on Linux is told very little:
//
//   - a host serves one device at a time. BlueZ does not say which central
//     wrote to a characteristic, so two devices on one host cannot be told
//     apart, and there is no per-device channel to answer on. The slot is
//     freed when the probe in internal/handlers/bluetooth notices the device
//     has gone quiet;
//   - nothing is announced. There is no beacon and no port: discovery is a BLE
//     scan for the service, so `cthrone scan --bluetooth` lists the devices in
//     radio range rather than the hosts on a network;
//   - a file is impractical. A packet carries 20 bytes, so a 256 KiB chunk is
//     thousands of notifications; text is what this transport is for.
//
// None of that changes the security model: every session is sealed by
// internal/e2e with the device key before a payload crosses, exactly as on a
// websocket, and a peer that cannot prove its key is refused rather than
// downgraded.
//
// The GATT layer is Linux only — it speaks BlueZ over the D-Bus system bus —
// so this package is built for Linux and unsupported.go answers the rest.
package bluetoothcore

import (
	"time"

	"github.com/fiwon123/cthrone/internal/e2e"
	bluetoothhandler "github.com/fiwon123/cthrone/internal/handlers/bluetooth"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// Confirm is the question a first contact is put to, the same one the
// websocket host asks. It is an alias rather than a type of its own so a caller
// can pass what it already builds for the websocket host.
type Confirm = func(e2e.FirstContact) bool

// The transport's timings. They are values rather than flags because each is a
// property of the radio, not a preference: a scan that gave up sooner would
// miss a device that was slow to advertise.
const (
	// dialTimeout is how long a connect waits for the host to appear.
	dialTimeout = 20 * time.Second

	// scanTimeout is how long `cthrone scan --bluetooth` listens.
	scanTimeout = 10 * time.Second
)

// Peer is a device a Bluetooth scan found: the address the connect names, the
// name the host advertises, and how strong its signal is.
//
// It is the handler's type rather than a copy, so one scan answers the terminal
// and the GUI without a conversion in between.
type Peer = bluetoothhandler.Peer

// seal wraps a transport session and refuses to hand out one that is not
// sealed. It is the same rule the other transports apply: a credential the
// handshake cannot use closes the session here rather than letting the caller
// use it in the clear.
func seal(raw session.Session, cred pairing.Credential, name string, gate Confirm) (session.Session, error) {
	cfg := cred.Config(e2e.Single, name)

	if gate != nil {
		cfg.Confirm = gate
	}

	sealed, err := e2e.New(raw, cfg)
	if err != nil {
		raw.Close(err.Error())

		return nil, err
	}

	return sealed, nil
}
