package e2e

import (
	"bytes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"unicode"

	"github.com/fiwon123/cthrone/internal/wire"
)

// The handshake, as two JSON payloads carried in wire frames of kind
// KindHandshake. There are two versions, and which one a session speaks is
// decided by the credential it was given:
//
//	version 1 — a shared secret:
//	{"v":1,"step":"hello","key":"<32 bytes>","nonce":"<32 bytes>","device":"name"}
//	{"v":1,"step":"confirm",...,"to":"<32 bytes>","mac":"<32 bytes>"}
//
//	version 2 — a device key, when no secret is configured:
//	{"v":2,"step":"hello",...,"id":"<32 bytes>","sig":"<64 bytes>"}
//	{"v":2,"step":"confirm",...,"id":"<32 bytes>","sig":"<64 bytes>","to":"<32 bytes>","mac":"<32 bytes>"}
//
// Each side sends a hello when it starts. A hello is answered with a confirm
// exactly once, and a confirm proves the sender holds what the version says:
// the shared secret in v1, the pairwise key derived from the exchange in v2,
// where the hello's signature has already proved which device is talking. It
// carries an HMAC over both hellos, addressed to the peer it is answering.
//
// A v2 confirm repeats the sender's identity and signature because a bus does
// not replay: the device that joins a subject second never sees the hello the
// first one already broadcast, only the confirm it receives in answer. A confirm
// that carried no identity could not be authenticated on its own, and a peer
// that joined a room a moment late could never pair. The signature is over the
// same hello claim — the confirm repeats the hello's key, nonce and name — so a
// lone confirm is exactly as strong as a hello and is recorded the same way.
//
// The address is in the clear as well as in the MAC. On a bus every confirm is
// broadcast to everyone, so a peer has to be able to tell "this one is not for
// me" from "this one is forged" before it spends anything on the second: the
// MAC alone cannot say, because it would have to be computed to find out.
const (
	// Version is the shared-secret handshake. It is what a configured secret
	// speaks, so an older peer and a newer one still meet on the secret.
	Version = 1

	// VersionIdentity is the device-key handshake. It is what a session with
	// no configured secret speaks: the hello carries the device's public key
	// and a signature over the hello, and the confirm key is derived from the
	// exchange instead of being the shared secret.
	VersionIdentity = 2

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

	// identityLabel separates a device signature from any other use of a
	// device key, so a hello signed once cannot be lifted into another role.
	identityLabel = " hello "

	// keyLabel separates the two directions of the session keys, so the key a
	// peer sends with is never the key it receives with.
	keyLabel = " key "

	// confirmCodeLabel separates the code two screens compare from every other
	// use of the transcript. It is a label rather than a field on the wire:
	// both sides derive the code locally and neither sends it.
	confirmCodeLabel = " confirm code "
)

// ProtocolIdentity names the device-key version. It takes the place of Protocol
// in the transcripts and derivations this version computes, so a value computed
// for one version is meaningless to the other.
const ProtocolIdentity = "cthrone-e2e/v2"

// handshake is one payload of the exchange.
//
// The byte fields are base64 in JSON, which is what the JSON encoder does for
// []byte, and the decoder does the opposite — a field that is not the size it
// must be is refused before it is used.
//
// Id and Sig are the device-key version's fields: the device's long-term public
// key and its signature over its hello claim. They are on both steps in v2 —
// a confirm is a hello that also answers one — and on neither step in v1.
type handshake struct {
	V      int    `json:"v"`
	Step   string `json:"step"`
	Key    []byte `json:"key"`
	Nonce  []byte `json:"nonce"`
	Device string `json:"device"`
	Id     []byte `json:"id,omitempty"`
	Sig    []byte `json:"sig,omitempty"`
	To     []byte `json:"to,omitempty"`
	MAC    []byte `json:"mac,omitempty"`
}

// decodeHandshake parses and validates one handshake payload. version is the
// handshake this side speaks, and a payload of another one is refused rather
// than guessed at.
func decodeHandshake(payload []byte, version int) (handshake, error) {
	var h handshake

	if err := json.Unmarshal(payload, &h); err != nil {
		return h, fmt.Errorf("%w: %v", ErrHandshake, err)
	}

	if h.V != version {
		return h, fmt.Errorf("%w: handshake version %d, this build speaks %d", ErrHandshake, h.V, version)
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

	if err := checkDeviceName(h.Device); err != nil {
		return h, err
	}

	if err := identityFields(h, version); err != nil {
		return h, err
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

// checkDeviceName refuses a name that would escape the line it is printed on.
//
// The name is chosen by the peer and shown verbatim on a terminal and in the
// app, so a carriage return or an escape sequence could forge a message or move
// the cursor. A device name is a label, not arbitrary text; the length bound
// above is about the buffer, this is about the screen.
func checkDeviceName(name string) error {
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: device name carries a control character", ErrHandshake)
		}
	}

	return nil
}

// identityFields checks that a step carries the identity fields its version
// calls for: every step of version 2 carries a device key and a signature (a
// confirm repeats them so it can stand alone), and no step of version 1 carries
// either. A peer cannot smuggle an identity into a version that does not check
// it, or into a step where it would go unverified.
func identityFields(h handshake, version int) error {
	if version == VersionIdentity {
		if len(h.Id) != ed25519.PublicKeySize {
			return fmt.Errorf("%w: device key is %d bytes, want %d", ErrHandshake, len(h.Id), ed25519.PublicKeySize)
		}

		if len(h.Sig) != ed25519.SignatureSize {
			return fmt.Errorf("%w: device signature is %d bytes, want %d", ErrHandshake, len(h.Sig), ed25519.SignatureSize)
		}

		return nil
	}

	if len(h.Id) != 0 || len(h.Sig) != 0 {
		return fmt.Errorf("%w: a %s of version %d carries no device identity", ErrHandshake, h.Step, version)
	}

	return nil
}

// helloClaim is what a device signs: the fields of its hello that the exchange
// depends on, under the device-key version's label. Signing the ephemeral key
// and the nonce is what binds the signature to this exchange — a signature
// lifted from another hello would be over different bytes.
func helloClaim(h handshake) []byte {
	out := make([]byte, 0, len(ProtocolIdentity)+len(identityLabel)+4+MaxDevice+KeySize+HandshakeNonce)
	out = append(out, ProtocolIdentity...)
	out = append(out, identityLabel...)
	out = appendName(out, h.Device)
	out = append(out, h.Key...)
	out = append(out, h.Nonce...)

	return out
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

// transcriptIdentity is the device-key version's transcript: the v1 fields with
// both long-term identities, in the same canonical order as everything else.
// Both handshakes must carry an Id — every step of version 2 does, so a confirm
// names the same device as the hello it repeats.
//
//	"cthrone-e2e/v2" ‖ len(name₁) ‖ name₁ ‖ id₁ ‖ key₁ ‖ nonce₁
//	                 ‖ len(name₂) ‖ name₂ ‖ id₂ ‖ key₂ ‖ nonce₂
func transcriptIdentity(a, b handshake) []byte {
	if bytes.Compare(a.Key, b.Key) > 0 {
		a, b = b, a
	}

	out := make([]byte, 0, len(ProtocolIdentity)+2*(4+MaxDevice+ed25519.PublicKeySize+2*KeySize))
	out = append(out, ProtocolIdentity...)
	out = appendName(out, a.Device)
	out = append(out, a.Id...)
	out = append(out, a.Key...)
	out = append(out, a.Nonce...)
	out = appendName(out, b.Device)
	out = append(out, b.Id...)
	out = append(out, b.Key...)
	out = append(out, b.Nonce...)

	return out
}

// deriveConfirmKey turns the pairwise X25519 secret into the key a confirm MAC
// is computed with in the device-key version.
//
// The shared secret was the confirm key in v1; here there is no shared secret,
// so the pairwise exchange has to produce one. It is derived from the same
// inputs as the session keys — the shared secret, both nonces — plus both
// identities, so a confirm is only valid between the two devices whose keys it
// names.
func deriveConfirmKey(shared, salt []byte, ours, theirs handshake) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, shared, salt, string(transcriptIdentity(ours, theirs))+macLabel, KeySize)
	if err != nil {
		return nil, fmt.Errorf("e2e: deriving the confirm key: %w", err)
	}

	return key, nil
}

// confirmCode is the six-digit code the two screens compare when the devices
// first meet.
//
// It is HKDF-SHA256 over the device-key transcript — the same bytes the confirm
// MAC covers — under a label of its own, so both sides compute it from what
// they have already recorded and neither sends it. It is a comparison, not a
// secret: it proves nothing on its own, and anyone who reads the hellos off the
// wire could compute it. What it buys is that a splice cannot make it agree —
// the two legs of a man in the middle have different ephemeral keys, so each
// leg derives a different code and the two screens show two different numbers.
//
// The shared-secret handshake has no code: every device holding the secret is
// the same identity, so there is no pair whose agreement means anything.
func confirmCode(ours, theirs handshake) (string, error) {
	key, err := hkdf.Key(sha256.New, transcriptIdentity(ours, theirs), nil, confirmCodeLabel, 8)
	if err != nil {
		return "", fmt.Errorf("e2e: deriving the first-contact code: %w", err)
	}

	// 900000 six-digit values, from the top of the range so a code never starts
	// with a zero: two leading zeros read as a shorter number out loud.
	return formatCode(binary.BigEndian.Uint64(key)%900000 + 100000), nil
}

// formatCode writes a six-digit number as two groups of three, which is the
// shape two people read aloud to each other.
func formatCode(n uint64) string {
	return fmt.Sprintf("%03d %03d", n/1000, n%1000)
}

// confirmMAC is the proof that the sender knows the shared secret.
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
