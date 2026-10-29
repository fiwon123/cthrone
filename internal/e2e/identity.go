package e2e

import (
	"crypto/ed25519"
	"errors"
)

// Identity is this device's long-term signing key.
//
// The shared secret is a credential every holder shares, so it can say "this
// peer may talk to us" but not "this is the same device as last time": three
// devices holding one secret are one identity with three homes. A device key is
// the opposite — one per device, never distributed — so a peer that saw it once
// can pin it and tell a returning device from an impostor using its name.
//
// A key is not a secret in the same sense as the shared secret: only the
// private half is, and it never leaves the device that made it.
type Identity struct {
	Key ed25519.PrivateKey
}

// Public is the half a peer sees and pins.
func (i Identity) Public() ed25519.PublicKey {
	if len(i.Key) != ed25519.PrivateKeySize {
		return nil
	}

	pub, _ := i.Key.Public().(ed25519.PublicKey)

	return pub
}

// Pins is where a first-contact identity is recorded and a returning one is
// checked. The exchange asks it twice: once when a peer's step arrives, to know
// whether the key is the one pinned for that device, and again after the
// exchange proved the peer holds the key, to record the pin.
//
// The two halves are separate on purpose. A hello names a device and signs it
// with a key of its own, which any stranger can do, so a pin recorded from a
// hello alone would let anyone claim a name and lock its real owner out. Check
// therefore writes nothing, and Pin is called only after the confirm
// authenticated the peer.
//
// A refusal is an error. It is what a changed key under a pinned name looks
// like, and it must stop the handshake rather than be pinned over.
type Pins interface {
	// Check reports whether pub is the key pinned for name and says whether
	// this is the first time the name has been seen. It writes nothing.
	Check(pub []byte, name string) (fingerprint string, first bool, err error)

	// Pin records a peer that has proved it holds pub. It creates the entry on
	// first contact and refreshes it on every later one.
	Pin(pub []byte, name string) error
}

// Errors of the identity mode. They are separate from ErrSecret and ErrMAC
// because the question is different: not "do you hold the secret?" but "are you
// the device you say you are?".
var (
	// ErrIdentity is a device key that cannot be used, or a peer that did not
	// present one, or one whose signature does not verify.
	ErrIdentity = errors.New("e2e: unusable device identity")

	// ErrPin is a peer the pin store refused: its name is already pinned to
	// another key.
	ErrPin = errors.New("e2e: peer identity refused")
)
