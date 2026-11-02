package bluetooth

import (
	"testing"
)

// FuzzReassemble drives the packet stream a radio peer controls: which message a
// packet claims to belong to, which packet of it it is, and how long the message
// says it will be.
//
// The packets arrive from a radio that cannot be asked to behave — a BLE
// peripheral has no idea who wrote to it — so a malformed stream has to be
// refused, never crash the host and never grow a buffer without end. The fuzzer
// feeds the stream in 20-byte packets, the size the transport uses (#418).
func FuzzReassemble(f *testing.F) {
	// A real message, so the fuzzer starts at completion rather than at the
	// first header check.
	valid := []byte("the quick brown fox jumps over the lazy dog, twice over, to need more than one packet")

	if packets, err := fragment(valid, 20, 7); err == nil {
		stream := make([]byte, 0, len(packets)*20)
		for _, packet := range packets {
			stream = append(stream, packet...)
		}

		f.Add(stream)
	}

	// A second message after the first, in the same stream: an id change and a
	// packet index reset are the interesting transition.
	if packets, err := fragment([]byte("one"), 20, 7); err == nil {
		stream := make([]byte, 0, 40)
		for _, packet := range packets {
			stream = append(stream, packet...)
		}

		if more, err := fragment([]byte("two"), 20, 8); err == nil {
			for _, packet := range more {
				stream = append(stream, packet...)
			}
		}

		f.Add(stream)
	}

	f.Add([]byte{})                                   // nothing at all
	f.Add([]byte{0, 0, 0})                            // shorter than the header
	f.Add([]byte{0, 1, 0, 0})                         // a first packet with no length
	f.Add([]byte{0, 1, 0, 0, 0xff, 0xff, 0xff, 0xff}) // a declared size past the limit
	f.Add([]byte{0, 0, 0, 5, 0, 0, 0, 1, 0x41})       // index 5, which starts no message

	f.Fuzz(func(t *testing.T, stream []byte) {
		r := &reassembler{}

		for len(stream) > 0 {
			size := 20
			if size > len(stream) {
				size = len(stream)
			}

			message, err := r.add(stream[:size])
			stream = stream[size:]

			if err != nil {
				// A refused packet is the normal answer; the caller closes the
				// link. Keep feeding anyway: a broken peer does exactly that
				// until the close lands, and it must not panic.
				continue
			}

			if len(message) > MaxMessage {
				t.Fatalf("reassembled %d bytes, past the %d-byte limit", len(message), MaxMessage)
			}
		}
	})
}
