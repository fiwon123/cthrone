package e2e

import (
	"bytes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/fiwon123/cthrone/internal/wire"
)

// The handshake, as two JSON payloads carried in wire frames of kind
// KindHandshake:
//
//	{"v":1,"step":"hello","key":"<32 bytes>","nonce":"<32 bytes>","device":"name"}
//	{"v":1,"step":"confirm",...,"to":"<32 bytes>","mac":"<32 bytes>"}
//
// Each side sends a hello when it starts. A hello is answered with a confirm
// exactly once, and a confirm proves the sender knows the shared secret: it
// carries an HMAC over both hellos, addressed to the peer it is answering.
//
// The address is in the clear as well as in the MAC. On a bus every confirm is
// broadcast to everyone, so a peer has to be able to tell "this one is not for
// me" from "this one is forged" before it spends anything on the second: the
// MAC alone cannot say, because it would have to be computed to find out.
const (
	// Version is the handshake version. A peer that speaks another one is
	// refused rather than guessed at.
	Version = 1

	// HandshakeNonce is how much randomness each side contributes. The two
	// nonces are the HKDF salt, so neither side alone decides the session keys.
	HandshakeNonce = 32

	// MaxDevice bounds the device name in a hello. The name is part of what the
	// MAC covers, so it has to be carried; the bound stops a peer from using it
	// as an unbounded field.
	MaxDevice = 128

	// stepHello opens the exchange, stepConfirm answers it.
	stepHello   = "hello"
	stepConfirm = "confirm"

	// macLabel separates a confirm MAC from any other use of the secret, so a
	// value computed for one purpose cannot be replayed as another.
	macLabel = " confirm "

	// keyLabel separates the two directions of the session keys, so the key a
	// peer sends with is never the key it receives with.
	keyLabel = " key "
)

// handshake is one payload of the exchange.
//
// The byte fields are base64 in JSON, which is what the JSON encoder does for
// []byte, and the decoder does the opposite — a field that is not the size it
// must be is refused before it is used.
type handshake struct {
	V      int    `json:"v"`
	Step   string `json:"step"`
	Key    []byte `json:"key"`
	Nonce  []byte `json:"nonce"`
	Device string `json:"device"`
	To     []byte `json:"to,omitempty"`
	MAC    []byte `json:"mac,omitempty"`
}

// decodeHandshake parses and validates one handshake payload.
func decodeHandshake(payload []byte) (handshake, error) {
	var h handshake

	if err := json.Unmarshal(payload, &h); err != nil {
		return h, fmt.Errorf("%w: %v", ErrHandshake, err)
	}

	if h.V != Version {
		return h, fmt.Errorf("%w: handshake version %d, this build speaks %d", ErrHandshake, h.V, Version)
	}

	if len(h.Key) != KeySize {
		return h, fmt.Errorf("%w: public key is %d bytes, want %d", ErrHandshake, len(h.Key), KeySize)
	}

	if len(h.Nonce) != HandshakeNonce {
		return h, fmt.Errorf("%w: nonce is %d bytes, want %d", ErrHandshake, len(h.Nonce), HandshakeNonce)
	}

	if len(h.Device) > MaxDevice {
		return h, fmt.Errorf("%w: device name is %d bytes, the limit is %d", ErrHandshake, len(h.Device), MaxDevice)
	}

	switch h.Step {
	case stepHello:
		if len(h.MAC) != 0 {
			return h, fmt.Errorf("%w: a hello carries no MAC", ErrHandshake)
		}

		if len(h.To) != 0 {
			return h, fmt.Errorf("%w: a hello is not addressed to one peer", ErrHandshake)
		}

		return h, nil

	case stepConfirm:
		if len(h.MAC) != sha256.Size {
			return h, fmt.Errorf("%w: confirm MAC is %d bytes, want %d", ErrHandshake, len(h.MAC), sha256.Size)
		}

		if len(h.To) != KeySize {
			return h, fmt.Errorf("%w: confirm is addressed to %d bytes, want a public key", ErrHandshake, len(h.To))
		}

		return h, nil

	default:
		return h, fmt.Errorf("%w: unknown step %q", ErrHandshake, h.Step)
	}
}

// transcript is the canonical text both sides MAC, in an order neither peer
// chooses: the two hellos sorted by public key. Sorting is what makes the two
// devices compute the same bytes without agreeing who started.
//
//	"cthrone-e2e/v1" ‖ len(name₁) ‖ name₁ ‖ key₁ ‖ nonce₁
//	                 ‖ len(name₂) ‖ name₂ ‖ key₂ ‖ nonce₂
//
// The name is length-prefixed because it is the only field a peer controls the
// length of, and every field after it has a fixed size.
func transcript(a, b handshake) []byte {
	if bytes.Compare(a.Key, b.Key) > 0 {
		a, b = b, a
	}

	out := make([]byte, 0, len(Protocol)+2*(4+MaxDevice+2*KeySize))
	out = append(out, Protocol...)
	out = appendName(out, a.Device)
	out = append(out, a.Key...)
	out = append(out, a.Nonce...)
	out = appendName(out, b.Device)
	out = append(out, b.Key...)
	out = append(out, b.Nonce...)

	return out
}

// appendName writes a four-byte big-endian length and then the name.
func appendName(dst []byte, name string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(name)))

	return append(dst, name...)
}

// confirmMAC is the proof that the sender knows the shared secret.
//
// It covers the transcript — both keys, both nonces, both names — and the
// public key of the peer it is addressed to. The recipient is what stops a
// reflection: a peer that is not the addressee cannot lift a confirm that was
// meant for someone else and use it on its own exchange, because the MAC it
// would have to produce is over a different key. Without that binding, two
// peers that both compute the same MAC could pass one back and forward and
// finish a handshake with a third party that never knew the secret.
func confirmMAC(secret, t, recipient []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(t)
	mac.Write([]byte(macLabel))
	mac.Write(recipient)

	return mac.Sum(nil)
}

// deriveKeys turns the X25519 shared secret into the two GCM ciphers a pair
// uses: ours for sending, theirs for receiving.
//
// Both sides derive both keys from the same inputs — the shared secret, the two
// nonces as salt, and a label naming whose key it is — so one side's send key
// is the other side's receive key and neither direction shares material with
// the other.
func deriveKeys(shared []byte, ours, theirs handshake) (send, recv cipher.AEAD, err error) {
	salt := handshakeSalt(ours, theirs)

	sendKey, err := hkdfKey(shared, salt, ours.Key)
	if err != nil {
		return nil, nil, err
	}

	recvKey, err := hkdfKey(shared, salt, theirs.Key)
	if err != nil {
		return nil, nil, err
	}

	send, err = newAEAD(sendKey)
	if err != nil {
		return nil, nil, err
	}

	recv, err = newAEAD(recvKey)
	if err != nil {
		return nil, nil, err
	}

	return send, recv, nil
}

// handshakeSalt is the two nonces in the same canonical order as the
// transcript, so both sides salt with the same bytes.
func handshakeSalt(a, b handshake) []byte {
	if bytes.Compare(a.Key, b.Key) > 0 {
		a, b = b, a
	}

	salt := make([]byte, 0, 2*HandshakeNonce)
	salt = append(salt, a.Nonce...)
	salt = append(salt, b.Nonce...)

	return salt
}

// hkdfKey derives the session key a payload is sealed with when its sender's
// public key is whose.
func hkdfKey(shared, salt, whose []byte) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, shared, salt, Protocol+keyLabel+string(whose), KeySize)
	if err != nil {
		return nil, fmt.Errorf("e2e: deriving a session key: %w", err)
	}

	return key, nil
}

// encodeHandshake is the wire form of one step: the JSON, in a handshake frame.
func encodeHandshake(h handshake) ([]byte, error) {
	payload, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding %s: %v", ErrHandshake, h.Step, err)
	}

	return wire.Encode(wire.Header{Kind: wire.KindHandshake}, payload)
}
