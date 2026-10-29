package e2e

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"testing"
)

// unitAEAD is a cipher for the tests that only exercise the seal format. The
// key never leaves this file, and it is not a session key.
func unitAEAD(t *testing.T) cipher.AEAD {
	t.Helper()

	aead, err := newAEAD(bytes.Repeat([]byte{0x2a}, KeySize))
	if err != nil {
		t.Fatalf("building a cipher: %v", err)
	}

	return aead
}

// unitTag is a recipient mark for the tests that do not have a peer.
func unitTag() []byte {
	return recipientTag(bytes.Repeat([]byte{0x3b}, KeySize))
}

func TestSealOpenRoundTrip(t *testing.T) {
	aead, tag := unitAEAD(t), unitTag()

	payload := []byte("a line of chat")

	frame, err := seal(aead, tag, firstSequence, payload)
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	if frame[0] != SealVersion {
		t.Fatalf("frame starts with 0x%02x, want 0x%02x", frame[0], SealVersion)
	}

	if want := 1 + TagLen + NonceSize + len(payload) + TagSize; len(frame) != want {
		t.Fatalf("frame is %d bytes, want %d", len(frame), want)
	}

	plain, seq, err := open(aead, frame)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}

	if !bytes.Equal(plain, payload) {
		t.Fatalf("opened %q, want %q", plain, payload)
	}

	if seq != firstSequence {
		t.Fatalf("sequence is %d, want %d", seq, firstSequence)
	}
}

func TestSealedHeaderNamesItsRecipient(t *testing.T) {
	aead, tag := unitAEAD(t), unitTag()

	frame, err := seal(aead, tag, 0x0102030405060708, []byte("for one peer"))
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	head, err := sealedHeader(frame)
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}

	if !bytes.Equal(head.tag, tag) {
		t.Fatalf("the frame is marked %x, want %x", head.tag, tag)
	}

	if head.seq != 0x0102030405060708 {
		t.Fatalf("the frame says sequence %d", head.seq)
	}
}

func TestRecipientMarkDependsOnTheKey(t *testing.T) {
	first := recipientTag(bytes.Repeat([]byte{0x01}, KeySize))
	second := recipientTag(bytes.Repeat([]byte{0x02}, KeySize))

	if len(first) != TagLen {
		t.Fatalf("the mark is %d bytes, want %d", len(first), TagLen)
	}

	if bytes.Equal(first, second) {
		t.Fatal("two peers share a mark")
	}

	if !bytes.Equal(first, recipientTag(bytes.Repeat([]byte{0x01}, KeySize))) {
		t.Fatal("the same key does not produce the same mark")
	}
}

func TestSealNeedsAMarkOfTheRightSize(t *testing.T) {
	aead := unitAEAD(t)

	if _, err := seal(aead, []byte{1, 2, 3}, firstSequence, nil); !errors.Is(err, ErrSeal) {
		t.Fatalf("sealing with a short mark returned %v, want ErrSeal", err)
	}
}

func TestSealNonceIsTheSequenceNumber(t *testing.T) {
	nonce := nonceFor(0x0102030405060708)

	if len(nonce) != NonceSize {
		t.Fatalf("nonce is %d bytes, want %d", len(nonce), NonceSize)
	}

	if !bytes.Equal(nonce[:8], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("nonce counter is %x", nonce[:8])
	}

	if !bytes.Equal(nonce[8:], []byte{0, 0, 0, 0}) {
		t.Fatalf("nonce tail is %x, want zeroes", nonce[8:])
	}
}

func TestSealBindsTheSequenceNumber(t *testing.T) {
	aead, tag := unitAEAD(t), unitTag()

	// A frame sealed at 7 does not open at 9: the number is the additional
	// data, so moving it along the stream fails instead of decrypting.
	frame, err := seal(aead, tag, 7, []byte("seventh"))
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	moved := append([]byte(nil), frame...)
	copy(moved[1+TagLen:], nonceFor(9))

	if _, _, err := open(aead, moved); !errors.Is(err, ErrAuth) {
		t.Fatalf("a frame moved to another number opened with %v", err)
	}
}

func TestSealRejectsTampering(t *testing.T) {
	aead, tag := unitAEAD(t), unitTag()

	frame, err := seal(aead, tag, firstSequence, []byte("a line of chat"))
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}

	frame[len(frame)-1] ^= 0xff

	if _, _, err := open(aead, frame); !errors.Is(err, ErrAuth) {
		t.Fatalf("a tampered frame opened with %v", err)
	}
}

func TestSealedHeaderRejectsTheWrongShape(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short", []byte{SealVersion, 0, 0}},
		{"no tag", append([]byte{SealVersion}, make([]byte, TagLen+NonceSize)...)},
		{"wrong version", append([]byte{0x02}, make([]byte, TagLen+NonceSize+TagSize)...)},
		{"text", []byte("a sentence, not a seal")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sealedHeader(tt.data); !errors.Is(err, ErrSeal) {
				t.Fatalf("sealedHeader(%q) = %v, want ErrSeal", tt.data, err)
			}
		})
	}
}

func TestSealWithoutAKeyFails(t *testing.T) {
	if _, err := seal(nil, unitTag(), firstSequence, []byte("data")); !errors.Is(err, ErrAuth) {
		t.Fatalf("sealing with no key returned %v, want ErrAuth", err)
	}
}
