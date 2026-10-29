package e2e

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// The sealed form of one payload:
//
//	0x01           1 byte   the seal version
//	tag            8 bytes  whose frame this is
//	nonce          12 bytes the sequence number, then four zero bytes
//	ciphertext     n bytes  the payload, encrypted and authenticated
//
// The first byte is what keeps a peer that does not encrypt from being read as
// one that does: no plaintext sentence starts with 0x01, so a receiver can tell
// "this was sealed" from "this was not" before it tries to decrypt anything,
// and refuse the second instead of printing it.
//
// The tag is why a bus works: the keys are pairwise, so one message becomes one
// frame per peer, and a peer has to be able to skip a frame that is not its
// own without mistaking it for a forgery it should refuse. It is a hash of the
// recipient's session key — a per-session pseudonym, not a device name — and it
// is only a hint: the key is still what opens the payload, so a tag that has
// been rewritten tells the wrong peer to try and fails there.
const (
	// SealVersion marks a sealed payload. A new seal layout takes a new value,
	// so both can be read side by side — the same rule as wire.Magic.
	SealVersion byte = 0x01

	// TagLen is the size of the recipient mark. Eight bytes of a SHA-256 is a
	// 2⁻⁶⁴ chance that two peers in one session share a mark, which is a
	// failed open and no worse.
	TagLen = 8

	// KeySize is the size of an AES-256-GCM key, one per direction.
	KeySize = 32

	// NonceSize is the GCM nonce: an eight-byte sequence number and four zero
	// bytes. The keys are fresh for every session, so the counter alone
	// guarantees a nonce is never reused — no randomness has to be trusted,
	// and a counter is the one thing a receiver can check.
	NonceSize = 12

	// TagSize is the GCM authentication tag, part of what Seal appends.
	TagSize = 16

	// seqSize is the size of the sequence number in the nonce and in the
	// additional data.
	seqSize = 8

	// firstSequence is the number of the first sealed payload in a direction.
	// Zero is never used, so an all-zero nonce never appears on the wire and a
	// counter of zero means "nothing sent".
	firstSequence uint64 = 1
)

// sealedFrame is the part of a sealed payload that is in the clear.
type sealedFrame struct {
	tag []byte
	seq uint64
}

// seal encrypts payload with aead at sequence number seq, marked for the peer
// whose tag is tag.
//
// The sequence number is the additional data, so a frame cannot be moved to
// another position in the stream: the receiver opens it at the number it
// expects, and a frame that was sealed at any other number fails closed.
func seal(aead cipher.AEAD, tag []byte, seq uint64, payload []byte) ([]byte, error) {
	if aead == nil {
		return nil, fmt.Errorf("%w: no key for this direction", ErrAuth)
	}

	if len(tag) != TagLen {
		return nil, fmt.Errorf("%w: recipient mark is %d bytes, want %d", ErrSeal, len(tag), TagLen)
	}

	nonce := nonceFor(seq)

	out := make([]byte, 0, 1+TagLen+NonceSize+len(payload)+aead.Overhead())
	out = append(out, SealVersion)
	out = append(out, tag...)
	out = append(out, nonce...)

	return aead.Seal(out, nonce, payload, sequence(seq)), nil
}

// open decrypts a sealed payload. It checks the version and the shape before
// it touches the key, so a truncated or unsealed frame is a clear error and not
// a GCM failure that says nothing.
func open(aead cipher.AEAD, data []byte) ([]byte, uint64, error) {
	head, err := sealedHeader(data)
	if err != nil {
		return nil, 0, err
	}

	start := 1 + TagLen + NonceSize

	payload, err := aead.Open(nil, data[1+TagLen:start], data[start:], sequence(head.seq))
	if err != nil {
		return nil, head.seq, fmt.Errorf("%w: the frame did not authenticate", ErrAuth)
	}

	return payload, head.seq, nil
}

// sealedHeader reads what a sealed payload says about itself without decrypting
// anything, so a receiver can skip a frame that is not addressed to it and
// refuse one that is out of order before it spends anything on either.
func sealedHeader(data []byte) (sealedFrame, error) {
	if len(data) < 1+TagLen+NonceSize+TagSize {
		return sealedFrame{}, fmt.Errorf("%w: %d bytes, too short for a sealed payload", ErrSeal, len(data))
	}

	if data[0] != SealVersion {
		return sealedFrame{}, fmt.Errorf("%w: seal version 0x%02x", ErrSeal, data[0])
	}

	return sealedFrame{
		tag: data[1 : 1+TagLen],
		seq: binary.BigEndian.Uint64(data[1+TagLen:]),
	}, nil
}

// recipientTag is the mark a payload for the peer holding key carries. The tag
// changes with the session, because the key it hashes does.
func recipientTag(key []byte) []byte {
	sum := sha256.Sum256(key)

	return sum[:TagLen]
}

// nonceFor is the GCM nonce of sequence number seq.
func nonceFor(seq uint64) []byte {
	nonce := make([]byte, NonceSize)
	binary.BigEndian.PutUint64(nonce, seq)

	return nonce
}

// sequence is the additional data belonging to seq.
func sequence(seq uint64) []byte {
	additional := make([]byte, seqSize)
	binary.BigEndian.PutUint64(additional, seq)

	return additional
}

// newAEAD turns a derived key into the cipher both directions use.
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("e2e: building the cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("e2e: building GCM: %w", err)
	}

	return aead, nil
}
