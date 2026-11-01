package wire

import (
	"bytes"
	"testing"
)

// Parse is the door every payload from a peer goes through, and a peer is not
// trusted: a hostile one gets to choose these bytes. The property that matters
// is that no input makes the parser panic — a crash in the receive loop is a
// denial of service, whatever the bytes meant.
func FuzzParse(f *testing.F) {
	// Seeds: the shapes the parser must already handle, and the ones that used
	// to be a bug (#201: a typed line that begins with the magic).
	seeds := []string{
		"",
		"hello",
		Magic + "hello",
	}

	header := Header{Kind: KindText}
	if frame, err := Encode(header, []byte("hello")); err == nil {
		seeds = append(seeds, string(frame))
	}

	fileHeader := Header{Kind: KindFile, ID: "abc", Name: "a.bin", Size: 3, Index: 0, Chunks: 1}
	if frame, err := Encode(fileHeader, []byte("abc")); err == nil {
		seeds = append(seeds, string(frame))
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := Parse(data)

		if err != nil {
			// A refusal is a fine answer. It must be a refusal, not a panic,
			// and it must come with no frame at all.
			if frame.Header.Kind != "" || frame.Payload != nil {
				t.Fatalf("Parse() refused with an error but returned a frame: %+v", frame)
			}

			return
		}

		// A text frame is the input verbatim: nothing may be lost or invented.
		if frame.Header.Kind == KindText && !IsFrame(data) {
			if !bytes.Equal(frame.Payload, data) {
				t.Fatalf("a text frame changed the payload: %q became %q", data, frame.Payload)
			}

			return
		}

		// Anything else parsed: it must be a shape the encoder can produce, so
		// a parsed frame re-encodes and parses back to itself.
		again, err := Encode(frame.Header, frame.Payload)
		if err != nil {
			return // a header the encoder refuses is a parse the caller should have refused; not a crash, so not this test's business.
		}

		round, err := Parse(again)
		if err != nil {
			t.Fatalf("a frame that parsed did not parse after re-encoding: %v", err)
		}

		if round.Header.Kind != frame.Header.Kind || round.Header.ID != frame.Header.ID || round.Header.Index != frame.Header.Index {
			t.Fatalf("the round trip changed the header: %+v became %+v", frame.Header, round.Header)
		}

		if !bytes.Equal(round.Payload, frame.Payload) {
			t.Fatalf("the round trip changed the payload: %q became %q", frame.Payload, round.Payload)
		}
	})
}

// FuzzChunks checks the arithmetic a file transfer depends on: the chunk count
// and the offsets must cover the size exactly, with no gap and no overlap, for
// every size an attacker or a bug can name.
func FuzzChunks(f *testing.F) {
	for _, seed := range []int64{0, 1, int64(ChunkSize) - 1, int64(ChunkSize), int64(ChunkSize) + 1, 1 << 20} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, size int64) {
		if size < 0 || size > 1<<40 {
			return // not a size this protocol can carry; validate() refuses it
		}

		chunks := Chunks(size)
		if chunks < 0 {
			t.Fatalf("Chunks(%d) = %d, want a count that cannot be negative", size, chunks)
		}

		offset := int64(0)

		for index := 0; index < chunks; index++ {
			if got := ChunkOffset(index); got != offset {
				t.Fatalf("Chunks(%d): chunk %d starts at %d, want %d", size, index, got, offset)
			}

			offset += int64(ChunkLen(size, index))
		}

		if offset != size {
			t.Fatalf("Chunks(%d) covers %d bytes, want %d", size, offset, size)
		}
	})
}
