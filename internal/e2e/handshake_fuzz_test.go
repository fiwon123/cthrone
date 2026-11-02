package e2e

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"testing"
)

// handshakeSeed builds a payload that decodeHandshake accepts, so the fuzzer
// starts inside the validation and mutates from there rather than spending its
// time on payloads that fail at the first length check.
func handshakeSeed(t testing.TB, h handshake) []byte {
	t.Helper()

	payload, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshalling a handshake seed: %v", err)
	}

	return payload
}

// FuzzHandshake is the first frame every transport reads, from a peer that has
// proved nothing yet: the hello and the confirm are parsed before any key is
// verified, so they are the program's most exposed bytes after the beacon
// (#414).
//
// A panic here is the finding; a refused handshake is the normal answer.
func FuzzHandshake(f *testing.F) {
	key := make([]byte, KeySize)
	nonce := make([]byte, HandshakeNonce)

	for i := range key {
		key[i] = byte(i)
	}

	for i := range nonce {
		nonce[i] = byte(255 - i)
	}

	helloV1 := handshake{V: Version, Step: stepHello, Key: key, Nonce: nonce, Device: "laptop"}

	helloV2 := helloV1
	helloV2.V = VersionIdentity
	helloV2.Id = make([]byte, ed25519.PublicKeySize)
	helloV2.Sig = make([]byte, ed25519.SignatureSize)

	confirmV2 := handshake{
		V:      VersionIdentity,
		Step:   stepConfirm,
		Key:    key,
		Nonce:  nonce,
		Device: "laptop",
		Id:     make([]byte, ed25519.PublicKeySize),
		Sig:    make([]byte, ed25519.SignatureSize),
		To:     key,
		MAC:    make([]byte, sha256.Size),
	}

	f.Add(handshakeSeed(f, helloV1))
	f.Add(handshakeSeed(f, helloV2))
	f.Add(handshakeSeed(f, confirmV2))
	f.Add([]byte(`{}`))
	f.Add([]byte(nil))
	f.Add([]byte(`not json`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		// Both versions are offered the same bytes: a peer picks the version it
		// speaks, and this side must refuse the other one without panicking.
		for _, version := range []int{Version, VersionIdentity} {
			_, _ = decodeHandshake(payload, version)
		}
	})
}
