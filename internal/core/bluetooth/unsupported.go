//go:build !linux || android

package bluetoothcore

import (
	"errors"
	"io"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// ErrUnsupported is what every entry point returns on a platform without the
// transport.
//
// The implementation talks to BlueZ over the D-Bus system bus, so it is Linux
// only. A build for Windows, macOS or Android therefore has no Bluetooth
// transport at all, and says so: a `--bluetooth` run fails with this message
// rather than with a flag that appears to work. Android is the one that will
// change: it needs its own binding to the platform's Bluetooth stack, which is
// a separate piece of work.
var ErrUnsupported = errors.New("bluetooth: this build has no Bluetooth transport — it speaks BlueZ over D-Bus, which only Linux provides")

// Supported reports that this build carries no Bluetooth transport at all.
//
// It is the other answer to the question supported_linux.go answers, and it
// exists so a caller can ask once instead of duplicating the platform rule: the
// GUI reads it to decide whether the transport is an option, and a build that
// says no here answers ErrUnsupported from every entry point below.
func Supported() bool { return false }

// HostHandle is the running host a caller can stop. There is never one on this
// platform; it exists so the callers compile the same way everywhere.
type HostHandle struct {
	DeviceFingerprint string
}

// Stop is what a caller does with the handle it never gets. It exists so the
// GUI's "the server I can stop" works out to the same type on every platform.
func (h *HostHandle) Stop() error { return ErrUnsupported }

// Host refuses: see ErrUnsupported.
func Host(app *app.Data, opts pairing.Options, confirm ...Confirm) error { return ErrUnsupported }

// OpenServer refuses: see ErrUnsupported.
func OpenServer(app *app.Data, opts pairing.Options, onSession func(session.Session), confirm ...Confirm) (*HostHandle, error) {
	return nil, ErrUnsupported
}

// Connect refuses: see ErrUnsupported.
func Connect(args []string, app *app.Data, opts pairing.Options) error { return ErrUnsupported }

// OpenSession refuses: see ErrUnsupported.
func OpenSession(args []string, app *app.Data, opts pairing.Options) (session.Session, error) {
	return nil, ErrUnsupported
}

// Scan refuses: see ErrUnsupported.
func Scan(out io.Writer) error { return ErrUnsupported }

// ScanPeers refuses: see ErrUnsupported.
func ScanPeers(timeout time.Duration, report func(Peer)) error {
	return ErrUnsupported
}
