package wire

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

// chunkHeader builds the header of one chunk of a file of size bytes. MIME is
// left for the caller when it matters; nothing in the framing depends on it.
func chunkHeader(id, name string, size int64, index int) Header {
	return Header{
		Kind:   KindFile,
		ID:     id,
		Name:   name,
		Size:   size,
		Index:  index,
		Chunks: Chunks(size),
	}
}

// endHeaderFor builds the file-end of a file whose bytes are data.
func endHeaderFor(id, name string, data []byte) Header {
	return Header{
		Kind:   KindFileEnd,
		ID:     id,
		Name:   name,
		Size:   int64(len(data)),
		Chunks: Chunks(int64(len(data))),
		SHA256: SumHex(sha256.Sum256(data)),
	}
}

// buildRaw writes a frame around header bytes the caller supplies, so a damaged
// header can be tested even though Encode would refuse to produce it.
func buildRaw(header, payload []byte) []byte {
	out := make([]byte, 0, len(Magic)+lengthLen+len(header)+len(payload))
	out = append(out, Magic...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(header)))
	out = append(out, header...)

	return append(out, payload...)
}

// TestEncodeParseRoundTrip covers every kind the envelope carries: text, each
// chunk of a file, and the end that closes it.
func TestEncodeParseRoundTrip(t *testing.T) {
	big := bytes.Repeat([]byte{0xA5}, 2*ChunkSize+7)

	tests := []struct {
		name    string
		header  Header
		payload []byte
	}{
		{"empty text", Header{Kind: KindText}, nil},
		{"text", Header{Kind: KindText}, []byte("hello")},
		{"text that is not UTF-8", Header{Kind: KindText}, []byte{0xff, 0xfe, 0x00, 0x80}},
		{"text at the chunk size", Header{Kind: KindText}, bytes.Repeat([]byte("x"), ChunkSize)},

		{"first chunk", chunkHeader("id-1", "movie.mp4", int64(len(big)), 0), big[:ChunkSize]},
		{"middle chunk", chunkHeader("id-1", "movie.mp4", int64(len(big)), 1), big[ChunkSize : 2*ChunkSize]},
		{"last chunk", chunkHeader("id-1", "movie.mp4", int64(len(big)), 2), big[2*ChunkSize:]},
		{"file-end", endHeaderFor("id-1", "movie.mp4", big), nil},

		{"a file of one full chunk", chunkHeader("id-2", "one.bin", ChunkSize, 0), bytes.Repeat([]byte{1}, ChunkSize)},
		{"a zero-byte file", endHeaderFor("id-3", "empty.txt", nil), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Encode(tt.header, tt.payload)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			got, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if got.Header != tt.header {
				t.Errorf("header = %+v, want %+v", got.Header, tt.header)
			}

			if !bytes.Equal(got.Payload, tt.payload) {
				t.Errorf("payload = %d bytes, want %d", len(got.Payload), len(tt.payload))
			}
		})
	}
}

// TestParseLeavesTextAlone is the compatibility promise: a peer that never
// wraps its text can still talk to this one.
func TestParseLeavesTextAlone(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"one byte", []byte("C")},
		{"part of the magic", []byte("CTH")},
		{"a chat about a similar string", []byte("CTH0 is not the magic")},
		{"not UTF-8", []byte{0x00, 0xff, 0xfe, 0x80}},
		{"a NUL in the middle", []byte("a\x00b")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.data)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if got.Header.Kind != KindText {
				t.Errorf("kind = %q, want %q", got.Header.Kind, KindText)
			}

			if !bytes.Equal(got.Payload, tt.data) {
				t.Errorf("payload = %q, want %q", got.Payload, tt.data)
			}
		})
	}
}

// TestParseRejectsDamagedFrames is the other half of that promise: anything
// that does start with the magic is a frame, and a damaged one is an error —
// not text the user might trust.
func TestParseRejectsDamagedFrames(t *testing.T) {
	goodChunk := chunkHeader("id-1", "x.bin", 10, 0)
	if err := goodChunk.validate(); err != nil {
		t.Fatalf("test header is invalid: %v", err)
	}

	goodJSON, err := json.Marshal(goodChunk)
	if err != nil {
		t.Fatalf("marshalling the test header: %v", err)
	}

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{
			name: "magic only",
			data: []byte(Magic),
			want: ErrFrame,
		},
		{
			name: "truncated header length",
			data: append([]byte(Magic), 0x00, 0x00),
			want: ErrFrame,
		},
		{
			name: "zero header length",
			data: buildRaw(nil, nil),
			want: ErrFrame,
		},
		{
			name: "header length past the data",
			data: append([]byte(Magic), 0x00, 0x00, 0x00, 0x10, '{', '}'),
			want: ErrFrame,
		},
		{
			name: "header length over the limit",
			data: append([]byte(Magic), 0x00, 0x01, 0x00, 0x01),
			want: ErrFrame,
		},
		{
			name: "header is not an object",
			data: buildRaw([]byte("[]"), nil),
			want: ErrFrame,
		},
		{
			name: "header is not JSON",
			data: buildRaw([]byte("{"), nil),
			want: ErrFrame,
		},
		{
			name: "a sentence that begins with the magic",
			data: []byte("CTH1 says hello"),
			want: ErrFrame,
		},
		{
			name: "unknown kind",
			data: buildRaw([]byte(`{"kind":"blob"}`), nil),
			want: ErrHeader,
		},
		{
			name: "file without an id",
			data: buildRaw([]byte(`{"kind":"file","name":"x","size":1,"chunks":1}`), []byte("x")),
			want: ErrHeader,
		},
		{
			name: "chunk with a short payload",
			data: buildRaw(goodJSON, nil),
			want: ErrPayload,
		},
		{
			name: "file-end with a payload",
			data: buildRaw(mustJSON(t, endHeaderFor("id-1", "x.bin", nil)), []byte("x")),
			want: ErrPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.data); !errors.Is(err, tt.want) {
				t.Errorf("Parse() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestEncodeRefusesToBuildBadFrames keeps the sender from producing something
// the receiver will reject. Each case is a rule Parse relies on.
func TestEncodeRefusesToBuildBadFrames(t *testing.T) {
	tests := []struct {
		name    string
		header  Header
		payload []byte
		want    error
	}{
		{
			name:    "text that starts with the magic",
			header:  Header{Kind: KindText},
			payload: []byte("CTH1 says hello"),
			want:    ErrFrame,
		},
		{
			name:   "unknown kind",
			header: Header{Kind: "blob"},
			want:   ErrHeader,
		},
		{
			name:   "file without an id",
			header: Header{Kind: KindFile, Name: "x", Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:   "file without a name",
			header: Header{Kind: KindFile, ID: "id-1", Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:   "file name with a slash",
			header: Header{Kind: KindFile, ID: "id-1", Name: "../escape", Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:   "file name with a backslash",
			header: Header{Kind: KindFile, ID: "id-1", Name: `..\escape`, Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:   "file name is a dot",
			header: Header{Kind: KindFile, ID: "id-1", Name: ".", Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:   "negative size",
			header: Header{Kind: KindFile, ID: "id-1", Name: "x", Size: -1, Chunks: 0},
			want:   ErrHeader,
		},
		{
			name:   "chunk count disagrees with the size",
			header: Header{Kind: KindFile, ID: "id-1", Name: "x", Size: 1, Chunks: 2},
			want:   ErrHeader,
		},
		{
			name:   "chunk index past the end",
			header: Header{Kind: KindFile, ID: "id-1", Name: "x", Size: 1, Chunks: 1, Index: 1},
			want:   ErrHeader,
		},
		{
			name:   "file-end without a hash",
			header: Header{Kind: KindFileEnd, ID: "id-1", Name: "x", Size: 1, Chunks: 1},
			want:   ErrHeader,
		},
		{
			name:    "file-end with a payload",
			header:  endHeaderFor("id-1", "x", nil),
			payload: []byte("x"),
			want:    ErrPayload,
		},
		{
			name:    "chunk payload too short",
			header:  chunkHeader("id-1", "x", 10, 0),
			payload: []byte("short"),
			want:    ErrPayload,
		},
		{
			name:    "chunk payload too long",
			header:  chunkHeader("id-1", "x", 1, 0),
			payload: []byte("too long"),
			want:    ErrPayload,
		},
		{
			name:   "handshake without a payload",
			header: Header{Kind: KindHandshake},
			want:   ErrPayload,
		},
		{
			name:    "handshake payload past the limit",
			header:  Header{Kind: KindHandshake},
			payload: make([]byte, MaxHandshake+1),
			want:    ErrPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Encode(tt.header, tt.payload); !errors.Is(err, tt.want) {
				t.Errorf("Encode() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestHandshakeFrameIsEnvelopeOnly covers the kind the end-to-end handshake
// rides on: this package moves the bytes and never reads them, so the only
// rules are that there are some and that they fit.
func TestHandshakeFrameIsEnvelopeOnly(t *testing.T) {
	payload := []byte(`{"v":1,"step":"hello"}`)

	frame, err := Encode(Header{Kind: KindHandshake}, payload)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	parsed, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if parsed.Header.Kind != KindHandshake {
		t.Fatalf("kind = %q, want %q", parsed.Header.Kind, KindHandshake)
	}

	if !bytes.Equal(parsed.Payload, payload) {
		t.Fatalf("payload = %q, want %q", parsed.Payload, payload)
	}
}

// TestChunks covers the arithmetic both ends depend on: a sender decides what
// to send by it, a receiver decides what should have arrived by it.
func TestChunks(t *testing.T) {
	tests := []struct {
		size int64
		want int
	}{
		{0, 0},
		{1, 1},
		{ChunkSize - 1, 1},
		{ChunkSize, 1},
		{ChunkSize + 1, 2},
		{2*ChunkSize + 7, 3},
		{-1, 0},
	}

	for _, tt := range tests {
		if got := Chunks(tt.size); got != tt.want {
			t.Errorf("Chunks(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

// TestChunkLenAndOffset covers the bounds of a chunk: full until the last one,
// and nothing for an index that is not a chunk.
func TestChunkLenAndOffset(t *testing.T) {
	size := int64(2*ChunkSize + 7)

	tests := []struct {
		index     int
		wantLen   int
		wantStart int64
	}{
		{0, ChunkSize, 0},
		{1, ChunkSize, ChunkSize},
		{2, 7, 2 * ChunkSize},
		{3, 0, 3 * ChunkSize},
		{-1, 0, -ChunkSize},
	}

	for _, tt := range tests {
		if got := ChunkLen(size, tt.index); got != tt.wantLen {
			t.Errorf("ChunkLen(%d, %d) = %d, want %d", size, tt.index, got, tt.wantLen)
		}

		if got := ChunkOffset(tt.index); got != tt.wantStart {
			t.Errorf("ChunkOffset(%d) = %d, want %d", tt.index, got, tt.wantStart)
		}
	}

	if got := ChunkLen(0, 0); got != 0 {
		t.Errorf("ChunkLen(0, 0) = %d, want 0", got)
	}
}

// TestSumHex pins the hash the empty file has, because a zero-byte transfer
// carries no bytes to check and its hash is the whole check.
func TestSumHex(t *testing.T) {
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	if got := SumHex(sha256.Sum256(nil)); got != empty {
		t.Errorf("SumHex(empty) = %s, want %s", got, empty)
	}

	if got := SumHex(sha256.Sum256([]byte("abc"))); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("SumHex(abc) = %s", got)
	}
}

func mustJSON(t *testing.T, h Header) []byte {
	t.Helper()

	out, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshalling %+v: %v", h, err)
	}

	return out
}
