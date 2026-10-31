// Package wire is the envelope two cthrone peers exchange, so one device can
// send another a file and not only a line of text.
//
// Everything the transports carry is already opaque bytes
// (session.Message.Payload), which is why a file is a framing problem and not a
// transport problem. A frame is:
//
//	"CTH1"          4 bytes  magic and version
//	header length   4 bytes  big-endian
//	header          JSON     {kind, id, name, size, mime, index, chunks, sha256}
//	payload         bytes    a chunk of the file, or the text of a message
//
// A payload that does not begin with the magic is text, byte for byte. That is
// what keeps a peer built before this package working: it receives the sentence
// the user typed, because that sentence was never wrapped. The other side of
// the rule is deliberate too — text that does begin with "CTH1" is read as a
// frame, which is why Encode refuses to send such text instead of handing the
// peer something it will misinterpret.
//
// One kind, KindHandshake, carries the end-to-end key exchange. Its payload is
// JSON that this package moves and never interprets: the envelope is the thing
// both peers already agree on, so the handshake rides it, and the cryptography
// stays in internal/e2e where it can be read on its own.
//
// Nothing in this package imports a transport or a GUI: the framing has to be
// testable on its own, and both paths and both front ends have to be able to
// use it. Assembler, which receives a file, lives in assembler.go.
package wire

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// Magic marks a frame and carries the format version. A payload without it
	// is text, so the magic may never change in place — a new format takes a
	// new magic and both versions can then be read side by side.
	Magic = "CTH1"

	// lengthLen is the size of the big-endian header length.
	lengthLen = 4

	// MaxHeader bounds the JSON header. A header is a few hundred bytes, so
	// anything near this is either a bug or an attempt to make the receiver
	// allocate: the length is read before the header is, and it is checked
	// before it is trusted.
	MaxHeader = 64 << 10

	// MaxHandshake bounds a handshake payload. Two public keys, two nonces, two
	// device names and a MAC are a few hundred bytes, so a frame near this is
	// not a handshake — it is something to refuse before parsing it as JSON.
	MaxHandshake = 4 << 10

	// ChunkSize is how much of a file one frame carries. The NATS broker's
	// max_payload defaults to 1 MiB, and the header plus websocket or TLS
	// framing has to fit beside the chunk, so 256 KiB leaves room for both and
	// still crosses a broker nobody configured.
	ChunkSize = 256 << 10
)

// Kind says what a frame carries.
type Kind string

const (
	// KindText is a chat message. It is never produced by Encode: text is sent
	// unwrapped so a peer that predates this package still reads it. The kind
	// still parses, so a future framing of text does not need a new magic.
	KindText Kind = "text"

	// KindFile is one chunk of a file, identified by ID and ordered by Index.
	KindFile Kind = "file"

	// KindFileEnd closes a transfer and carries the SHA-256 of the whole file.
	// The receiver checks it before the file is offered to anyone.
	KindFileEnd Kind = "file-end"

	// KindHandshake is one step of the end-to-end key exchange. Its payload is
	// JSON and this package does not read it: the envelope carries the bytes,
	// internal/e2e decides what they mean, and a peer that does not expect a
	// handshake refuses the frame instead of printing base64 into the chat.
	KindHandshake Kind = "handshake"
)

// Header is the JSON that precedes the payload of a frame.
//
// Size, Chunks and SHA256 describe the whole file and are repeated on every
// frame, not accumulated per chunk: a chunk that disagrees with the one before
// it describes a broken stream, and the receiver says so instead of writing it.
type Header struct {
	Kind   Kind   `json:"kind"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size,omitempty"`
	MIME   string `json:"mime,omitempty"`
	Index  int    `json:"index,omitempty"`
	Chunks int    `json:"chunks,omitempty"`
	SHA256 string `json:"sha256,omitempty"`

	// Sender is who sent a frame a host relayed, when it was not the host. A
	// file has no line to prefix the way text does, so the name travels in the
	// header. It is optional and additive: a peer built before this field
	// existed ignores it and still reads the frame, which is why it is a field
	// rather than a new frame kind — a new kind would make an older peer refuse
	// the transfer.
	Sender string `json:"sender,omitempty"`
}

// Frame is a parsed message: the header and the bytes it describes.
type Frame struct {
	Header  Header
	Payload []byte
}

// Errors the callers switch on. A message says what was wrong; the error says
// what kind of wrong it was, so a receiver can discard a transfer on any of
// them without reading English.
var (
	// ErrFrame is a byte string that starts with the magic and is not a frame.
	ErrFrame = errors.New("wire: malformed frame")

	// ErrHeader is a header that parses but describes something invalid.
	ErrHeader = errors.New("wire: invalid header")

	// ErrPayload is a payload whose length does not match its header.
	ErrPayload = errors.New("wire: invalid payload")

	// ErrIntegrity is a file that arrived with the wrong size or hash.
	ErrIntegrity = errors.New("wire: integrity check failed")

	// ErrSequence is a chunk that arrived out of order, or a transfer that
	// ended before it was complete.
	ErrSequence = errors.New("wire: chunk out of sequence")

	// ErrNotTransfer is a text message handed to something that only takes
	// files.
	ErrNotTransfer = errors.New("wire: not a file transfer")
)

// IsFrame reports whether data is marked as a frame. It answers false for text,
// including text that is only part of the magic.
func IsFrame(data []byte) bool {
	return len(data) >= len(Magic) && string(data[:len(Magic)]) == Magic
}

// Encode serialises a frame.
//
// Text is returned unchanged: it is not a frame, and wrapping it would break
// the peer that predates this format. For that reason, text that begins with
// the magic is refused — the receiver would read it as a frame, so the honest
// failure is here, where the user can be told.
func Encode(h Header, payload []byte) ([]byte, error) {
	if h.Kind == KindText {
		if IsFrame(payload) {
			return nil, fmt.Errorf("%w: text beginning with %q would be read as a frame", ErrFrame, Magic)
		}

		return payload, nil
	}

	if err := h.validate(); err != nil {
		return nil, err
	}

	if err := h.checkPayload(len(payload)); err != nil {
		return nil, err
	}

	header, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding the header: %v", ErrHeader, err)
	}

	if len(header) > MaxHeader {
		return nil, fmt.Errorf("%w: header is %d bytes, the limit is %d", ErrHeader, len(header), MaxHeader)
	}

	out := make([]byte, 0, len(Magic)+lengthLen+len(header)+len(payload))
	out = append(out, Magic...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(header)))
	out = append(out, header...)
	out = append(out, payload...)

	return out, nil
}

// Parse decodes data into a frame.
//
// A payload that does not begin with the magic is text, returned unchanged and
// without a copy: the transports hand the receiver one buffer per message, and
// nothing here needs to own it. A payload that does begin with the magic is a
// frame, and a damaged one is an error rather than a line of text — silently
// falling back would turn a bug into a message the user cannot tell from a real
// one.
func Parse(data []byte) (Frame, error) {
	if !IsFrame(data) {
		return Frame{Header: Header{Kind: KindText}, Payload: data}, nil
	}

	if len(data) < len(Magic)+lengthLen {
		return Frame{}, fmt.Errorf("%w: %d bytes, too short for a header length", ErrFrame, len(data))
	}

	size := binary.BigEndian.Uint32(data[len(Magic):])
	start := len(Magic) + lengthLen

	if size == 0 {
		return Frame{}, fmt.Errorf("%w: header length is zero", ErrFrame)
	}

	if size > MaxHeader {
		return Frame{}, fmt.Errorf("%w: header length %d exceeds the %d-byte limit", ErrFrame, size, MaxHeader)
	}

	if start+int(size) > len(data) {
		return Frame{}, fmt.Errorf("%w: header length %d, but only %d bytes follow", ErrFrame, size, len(data)-start)
	}

	var h Header
	if err := json.Unmarshal(data[start:start+int(size)], &h); err != nil {
		return Frame{}, fmt.Errorf("%w: decoding the header: %v", ErrFrame, err)
	}

	if err := h.validate(); err != nil {
		return Frame{}, err
	}

	payload := data[start+int(size):]
	if err := h.checkPayload(len(payload)); err != nil {
		return Frame{}, err
	}

	return Frame{Header: h, Payload: payload}, nil
}

// Chunks is how many chunks a file of size bytes takes.
//
// A zero-byte file takes none: there is nothing to carry, and the transfer is
// just its file-end. A file that fits exactly takes one, not two.
func Chunks(size int64) int {
	if size <= 0 {
		return 0
	}

	return int((size + ChunkSize - 1) / ChunkSize)
}

// ChunkOffset is where chunk index starts in the file.
func ChunkOffset(index int) int64 {
	return int64(index) * ChunkSize
}

// ChunkLen is how many bytes chunk index of a file of size bytes carries, or
// zero when that chunk does not exist.
func ChunkLen(size int64, index int) int {
	if index < 0 || index >= Chunks(size) {
		return 0
	}

	remaining := size - ChunkOffset(index)
	if remaining > ChunkSize {
		return ChunkSize
	}

	return int(remaining)
}

// validate reports whether the header describes something a peer can act on.
// It checks the whole-file fields on every kind: a chunk that disagrees with
// its siblings is how a receiver notices a broken stream.
func (h Header) validate() error {
	switch h.Kind {
	case KindText:
		return nil

	case KindHandshake:
		// The bytes are opaque to this package on purpose, so there is nothing
		// in the header to check: a handshake carries no file and no id.
		return nil

	case KindFile, KindFileEnd:
		if h.ID == "" {
			return fmt.Errorf("%w: %s frame without an id", ErrHeader, h.Kind)
		}

		if h.Size < 0 {
			return fmt.Errorf("%w: %s frame for %q has size %d", ErrHeader, h.Kind, h.ID, h.Size)
		}

		if want := Chunks(h.Size); h.Chunks != want {
			return fmt.Errorf("%w: %q says %d chunks, %d bytes take %d", ErrHeader, h.ID, h.Chunks, h.Size, want)
		}

		if err := checkName(h.Name); err != nil {
			return err
		}

		if h.Kind == KindFile {
			if h.Index < 0 || h.Index >= h.Chunks {
				return fmt.Errorf("%w: %q chunk index %d, with %d chunks", ErrHeader, h.ID, h.Index, h.Chunks)
			}

			return nil
		}

		if !validSum(h.SHA256) {
			return fmt.Errorf("%w: %q ends with %q, not a SHA-256", ErrHeader, h.ID, h.SHA256)
		}

		return nil

	default:
		return fmt.Errorf("%w: unknown kind %q", ErrHeader, h.Kind)
	}
}

// checkPayload reports whether a payload of this length can belong to the
// header. A chunk has an exact length the receiver can predict, which is what
// turns a truncated frame or a swapped chunk into an error before it reaches
// the disk.
func (h Header) checkPayload(length int) error {
	switch h.Kind {
	case KindText:
		return nil

	case KindHandshake:
		if length == 0 {
			return fmt.Errorf("%w: handshake frame with an empty payload", ErrPayload)
		}

		if length > MaxHandshake {
			return fmt.Errorf("%w: handshake frame is %d bytes, the limit is %d", ErrPayload, length, MaxHandshake)
		}

		return nil

	case KindFile:
		if want := ChunkLen(h.Size, h.Index); length != want {
			return fmt.Errorf("%w: chunk %d of %q is %d bytes, want %d",
				ErrPayload, h.Index, h.ID, length, want)
		}

		return nil

	case KindFileEnd:
		if length != 0 {
			return fmt.Errorf("%w: file-end for %q carries %d bytes", ErrPayload, h.ID, length)
		}

		return nil

	default:
		return fmt.Errorf("%w: unknown kind %q", ErrHeader, h.Kind)
	}
}

// checkName refuses a name a receiver cannot safely write. A peer chooses this
// string, so it is the one field that could point outside the download folder.
func checkName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: file without a name", ErrHeader)
	}

	if name == "." || name == ".." {
		return fmt.Errorf("%w: file name %q", ErrHeader, name)
	}

	if strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: file name %q contains a path separator", ErrHeader, name)
	}

	if filepath.Base(name) != name {
		return fmt.Errorf("%w: file name %q is a path", ErrHeader, name)
	}

	return nil
}

// validSum reports whether s is a SHA-256 in hex. Case is not part of the
// contract — a peer that hex-encodes with capitals is readable — so comparisons
// use strings.EqualFold.
func validSum(s string) bool {
	if len(s) != hex.EncodedLen(32) {
		return false
	}

	_, err := hex.DecodeString(s)

	return err == nil
}

// SumHex is the header form of a file's SHA-256: lowercase hex.
func SumHex(sum [32]byte) string {
	return hex.EncodeToString(sum[:])
}
