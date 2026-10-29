package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fiwon123/cthrone/internal/wire"
)

// hellos builds the two sides of an exchange with fixed, comparable fields.
func hellos() (a, b handshake) {
	a = handshake{
		V:      Version,
		Step:   stepHello,
		Key:    bytes.Repeat([]byte{0x11}, KeySize),
		Nonce:  bytes.Repeat([]byte{0x21}, HandshakeNonce),
		Device: "alice",
	}
	b = handshake{
		V:      Version,
		Step:   stepHello,
		Key:    bytes.Repeat([]byte{0x31}, KeySize),
		Nonce:  bytes.Repeat([]byte{0x41}, HandshakeNonce),
		Device: "bob",
	}

	return a, b
}

func TestTranscriptIsTheSameOnBothSides(t *testing.T) {
	a, b := hellos()

	if !bytes.Equal(transcript(a, b), transcript(b, a)) {
		t.Fatal("the two sides compute different transcripts")
	}
}

func TestTranscriptCoversEveryField(t *testing.T) {
	a, b := hellos()
	want := transcript(a, b)

	tests := []struct {
		name   string
		mangle func(h *handshake)
	}{
		{"device", func(h *handshake) { h.Device = "mallory" }},
		{"key", func(h *handshake) { h.Key[0] ^= 0xff }},
		{"nonce", func(h *handshake) { h.Nonce[0] ^= 0xff }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := b
			changed.Key = append([]byte(nil), b.Key...)
			changed.Nonce = append([]byte(nil), b.Nonce...)
			tt.mangle(&changed)

			if bytes.Equal(transcript(a, changed), want) {
				t.Fatalf("changing the %s left the transcript alone", tt.name)
			}
		})
	}
}

func TestTranscriptSeparatesFieldsByName(t *testing.T) {
	a, b := hellos()

	// Without the length prefix, moving a byte from a device name into a key
	// would produce the same bytes. The prefix is what makes it two values.
	a.Device = "ab"

	out := transcript(a, b)

	if !bytes.Contains(out, append([]byte{0, 0, 0, 2}, []byte("ab")...)) {
		t.Fatalf("the transcript does not length-prefix the device name: %x", out)
	}

	if !bytes.Equal(transcript(a, b), transcript(b, a)) {
		t.Fatal("the canonical order depends on the argument order")
	}
}

func TestConfirmMACBindsTheRecipient(t *testing.T) {
	a, b := hellos()
	shared := transcript(a, b)

	toAlice := confirmMAC(secret, shared, a.Key)
	toBob := confirmMAC(secret, shared, b.Key)

	if bytes.Equal(toAlice, toBob) {
		t.Fatal("a confirm is valid for any peer")
	}

	// The reflection: a peer lifts the confirm that was addressed to someone
	// else and presents it as its own. The MAC it would need is over a
	// different key, so the check fails.
	if bytes.Equal(toAlice, confirmMAC(secret, shared, bytes.Repeat([]byte{0x99}, KeySize))) {
		t.Fatal("a confirm computed for one peer verified for another")
	}

	if bytes.Equal(toAlice, confirmMAC([]byte("a-different-shared-secret-02"), shared, a.Key)) {
		t.Fatal("a confirm verified under the wrong secret")
	}
}

func TestDerivedKeysAreDirectional(t *testing.T) {
	a, b := hellos()
	shared := bytes.Repeat([]byte{0x5a}, KeySize)

	sendA, recvA, err := deriveKeys(shared, a, b)
	if err != nil {
		t.Fatalf("deriving alice's keys: %v", err)
	}

	sendB, recvB, err := deriveKeys(shared, b, a)
	if err != nil {
		t.Fatalf("deriving bob's keys: %v", err)
	}

	frame, err := seal(sendA, recipientTag(b.Key), firstSequence, []byte("from alice"))
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	plain, _, err := open(recvB, frame)
	if err != nil {
		t.Fatalf("bob opens what alice sealed: %v", err)
	}

	if string(plain) != "from alice" {
		t.Fatalf("bob opened %q", plain)
	}

	// Alice cannot open her own frame with her own key: the two directions
	// share no material, so a frame from one is unreadable by the other.
	if _, _, err := open(recvA, frame); !errors.Is(err, ErrAuth) {
		t.Fatalf("alice opened her own frame with her receive key: %v", err)
	}

	if _, _, err := open(sendB, frame); !errors.Is(err, ErrAuth) {
		t.Fatalf("bob opened alice's frame with his send key: %v", err)
	}
}

func TestDerivedKeysNeedBothNonces(t *testing.T) {
	a, b := hellos()
	shared := bytes.Repeat([]byte{0x5a}, KeySize)

	sendA, _, err := deriveKeys(shared, a, b)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}

	// A salt that carried only the peer's nonce would let a third party land on
	// the same keys; both nonces go in, in the order the transcript uses.
	other := b
	other.Nonce = bytes.Repeat([]byte{0x42}, HandshakeNonce)

	_, thirdRecv, err := deriveKeys(shared, a, other)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}

	frame, err := seal(sendA, recipientTag(b.Key), firstSequence, []byte("from alice"))
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	if _, _, err := open(thirdRecv, frame); !errors.Is(err, ErrAuth) {
		t.Fatalf("a salt without both nonces opened the frame: %v", err)
	}
}

func TestDecodeHandshake(t *testing.T) {
	valid, _ := hellos()

	confirm := handshake{
		V:      Version,
		Step:   stepConfirm,
		Key:    valid.Key,
		Nonce:  valid.Nonce,
		Device: valid.Device,
		To:     bytes.Repeat([]byte{0x51}, KeySize),
		MAC:    bytes.Repeat([]byte{0x01}, 32),
	}

	encode := func(h handshake) []byte {
		out, err := json.Marshal(h)
		if err != nil {
			t.Fatalf("marshalling: %v", err)
		}

		return out
	}

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{"hello", encode(valid), false},
		{"confirm", encode(confirm), false},
		{"not json", []byte("hello?"), true},
		{"no version", encode(handshake{Step: stepHello, Key: valid.Key, Nonce: valid.Nonce}), true},
		{"other version", encode(handshake{V: Version + 1, Step: stepHello, Key: valid.Key, Nonce: valid.Nonce}), true},
		{"short key", encode(handshake{V: Version, Step: stepHello, Key: valid.Key[:16], Nonce: valid.Nonce}), true},
		{"short nonce", encode(handshake{V: Version, Step: stepHello, Key: valid.Key, Nonce: valid.Nonce[:16]}), true},
		{"long device", encode(handshake{V: Version, Step: stepHello, Key: valid.Key, Nonce: valid.Nonce,
			Device: string(bytes.Repeat([]byte("x"), MaxDevice+1))}), true},
		{"hello with a MAC", encode(handshake{V: Version, Step: stepHello, Key: valid.Key, Nonce: valid.Nonce, MAC: confirm.MAC}), true},
		{"confirm without a MAC", encode(handshake{V: Version, Step: stepConfirm, Key: valid.Key, Nonce: valid.Nonce, To: confirm.To}), true},
		{"confirm without an address", encode(handshake{V: Version, Step: stepConfirm, Key: valid.Key, Nonce: valid.Nonce, MAC: confirm.MAC}), true},
		{"hello with an address", encode(handshake{V: Version, Step: stepHello, Key: valid.Key, Nonce: valid.Nonce, To: confirm.To}), true},
		{"unknown step", encode(handshake{V: Version, Step: "rekey", Key: valid.Key, Nonce: valid.Nonce}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeHandshake(tt.payload)

			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeHandshake(%s) = %v, wantErr %v", tt.name, err, tt.wantErr)
			}

			if err != nil && !errors.Is(err, ErrHandshake) {
				t.Fatalf("decodeHandshake(%s) = %v, want ErrHandshake", tt.name, err)
			}
		})
	}
}

func TestEncodeHandshakeCarriesTheStep(t *testing.T) {
	a, _ := hellos()

	frame, err := encodeHandshake(a)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	parsed, err := wire.Parse(frame)
	if err != nil {
		t.Fatalf("parsing the frame back: %v", err)
	}

	if parsed.Header.Kind != wire.KindHandshake {
		t.Fatalf("the frame is a %q, want a handshake", parsed.Header.Kind)
	}

	h, err := decodeHandshake(parsed.Payload)
	if err != nil {
		t.Fatalf("decoding the step: %v", err)
	}

	if h.Step != stepHello || h.Device != a.Device {
		t.Fatalf("read back %q from %q", h.Step, h.Device)
	}
}

func TestPeerNameHasAFallback(t *testing.T) {
	if got := peerName(""); got == "" {
		t.Fatal("an unnamed peer has no name for the message")
	}

	if got := peerName("bob"); got != "bob" {
		t.Fatalf("peerName(bob) = %q", got)
	}
}
