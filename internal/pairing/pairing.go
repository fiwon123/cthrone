// Package pairing resolves the credential a session authenticates with.
//
// There are two, and exactly one is used:
//
//   - a shared secret, when one is configured — a flag, the environment or a
//     file. Every device that holds it is the same identity, which is what
//     v0.6.0 peers know, so a session with a secret keeps speaking the v1
//     handshake;
//   - this device's own key, when no secret is configured. The key is never
//     distributed: a peer pins it on first contact and refuses a different one
//     under the same name. That is what lets two devices pair with no value
//     copied between them, which a phone needs because it has no terminal and
//     its clipboard does not cross devices.
//
// The choice is made here, once, so the websocket and NATS paths cannot
// disagree about which model a run is using.
package pairing

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
)

// SourceDeviceKey is the source of a credential that is this device's own key.
// The sources of a shared secret are the ones auth reports.
const SourceDeviceKey = "device key"

// Credential is what a session authenticates with: a shared secret, or this
// device's key. Both carry the pin store — the device pins in the key model,
// and the host certificate pinned on first contact in both — because the
// websocket transport contacts a host before the handshake that says which
// model the peer speaks. It is never empty.
type Credential struct {
	secret   string
	identity *e2e.Identity
	pins     *discovery.PinStore
	source   string
}

// HasSecret reports whether the credential is the shared secret. A session
// with one speaks the v1 handshake, and the websocket host keeps its header
// check; without one it speaks the device-key handshake and the end-to-end
// layer is the gate.
func (c Credential) HasSecret() bool { return c.secret != "" }

// Secret is the shared secret, or the empty string in the device-key model.
func (c Credential) Secret() string { return c.secret }

// Source says where the credential came from, for a status line.
func (c Credential) Source() string { return c.source }

// Pins is the trust-on-first-use record of the devices and host certificates
// this device has met, or nil for a credential that was never loaded. It is
// set in both models: the websocket client pins its host's certificate
// through it before the handshake says which credential the peer speaks.
func (c Credential) Pins() *discovery.PinStore { return c.pins }

// Fingerprint is this device's own fingerprint in the device-key model: what
// the other screen shows when the two devices first meet. It is empty when a
// shared secret is used, which has no per-device identity to show.
func (c Credential) Fingerprint() string {
	if c.identity == nil {
		return ""
	}

	return discovery.Fingerprint(c.identity.Public())
}

// Config is the end-to-end configuration this credential speaks. mode is the
// transport's: Single on a websocket, Bus on NATS.
func (c Credential) Config(mode e2e.Mode, name string) e2e.Config {
	if c.identity == nil {
		return e2e.Config{Secret: []byte(c.secret), Name: name, Mode: mode}
	}

	return e2e.Config{Identity: c.identity, Pins: c.pins, Name: name, Mode: mode}
}

// Options names every source a credential may come from. An empty path means
// the default file for that kind, which is what the CLI uses; the GUI passes
// its own directory because a phone has no ~/.config to rely on.
type Options struct {
	// FlagToken is the --token value. It is the least private source, and the
	// most explicit, so it wins.
	FlagToken string

	// TokenFile is the file the shared secret is read from.
	TokenFile string

	// KeyFile is this device's long-term key, generated on first use.
	KeyFile string

	// PinsFile is where first contacts are recorded.
	PinsFile string
}

// Load resolves the credential: the shared secret if one is configured, and
// this device's key with its pins otherwise.
//
// The pins are loaded in both cases. The device key needs them to pin peers,
// and the certificate of the host is trusted on first use through them, which
// happens before the handshake says which model the peer speaks.
//
// A secret that is configured but unreadable is an error rather than a reason
// to fall back to pairing: the user meant to use the secret, and a device that
// quietly paired instead would have a pin store nobody asked for.
func Load(opts Options) (Credential, error) {
	tokenFile, err := tokenPath(opts.TokenFile)
	if err != nil {
		return Credential{}, err
	}

	token, err := auth.Resolve(opts.FlagToken, tokenFile)
	if err != nil {
		return Credential{}, err
	}

	pinsFile, err := pinsPath(opts.PinsFile)
	if err != nil {
		return Credential{}, err
	}

	pins, err := discovery.NewPinStore(pinsFile)
	if err != nil {
		return Credential{}, err
	}

	if token.Value != "" {
		// The secret is checked here, not when the first peer arrives: a
		// secret too short to derive a key from would otherwise fail inside
		// the host's per-connection wrapper, where nobody reads the error and
		// every client is dropped in silence.
		if len(token.Value) < e2e.MinSecret {
			return Credential{}, fmt.Errorf("%w: the shared secret is %d bytes, the handshake needs at least %d",
				e2e.ErrSecret, len(token.Value), e2e.MinSecret)
		}

		return Credential{secret: token.Value, pins: pins, source: token.Source}, nil
	}

	keyFile, err := keyPath(opts.KeyFile)
	if err != nil {
		return Credential{}, err
	}

	key, err := discovery.LoadKeyFrom(keyFile)
	if err != nil {
		return Credential{}, err
	}

	return Credential{
		identity: &e2e.Identity{Key: key},
		pins:     pins,
		source:   SourceDeviceKey,
	}, nil
}

// tokenPath is the file a shared secret is read from. A caller that names no
// file gets the default one, so the host and the client on one machine agree
// without a flag.
func tokenPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return auth.DefaultFile()
}

// keyPath is this device's key file.
func keyPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return discovery.IdentityKeyFile()
}

// pinsPath is the file first contacts are recorded in.
func pinsPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return discovery.DefaultPinsFile()
}

// String names the credential the way a status line can use it.
func (c Credential) String() string {
	if c.HasSecret() {
		return fmt.Sprintf("shared secret (%s)", c.source)
	}

	return fmt.Sprintf("%s %s", SourceDeviceKey, c.Fingerprint())
}
