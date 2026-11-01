package bluetooth

import (
	"bytes"
	"errors"
	"testing"
)

// A message survives the round trip at every packet size a BLE link offers,
// including the smallest the header allows and a payload that fills its last
// packet exactly.
func TestFragmentRoundTrip(t *testing.T) {
	sizes := []int{9, 10, 20, 23, 64, 185, 512}

	payloads := map[string][]byte{
		"empty":          {},
		"one byte":       {'x'},
		"a sentence":     []byte("morning, bob — the network is down again"),
		"not UTF-8":      {0x00, 0xff, 0x80, 0x01},
		"larger":         bytes.Repeat([]byte("packets "), 400),
		"a file chunk":   bytes.Repeat([]byte{0xA5}, 256<<10),
		"exactly filled": bytes.Repeat([]byte("y"), 116),
	}

	for _, mtu := range sizes {
		for name, payload := range payloads {
			t.Run(name+"/"+itoa(mtu), func(t *testing.T) {
				// The MTU has to carry the header and a little more.
				if mtu <= headerLen+lengthLen {
					return
				}

				packets, err := fragment(payload, mtu, 7)
				if err != nil {
					t.Fatalf("fragment() error = %v", err)
				}

				if len(packets) == 0 {
					t.Fatal("fragment() produced no packets")
				}

				var r reassembler

				var got []byte

				for i, packet := range packets {
					if len(packet) > mtu {
						t.Fatalf("packet %d is %d bytes, the MTU is %d", i, len(packet), mtu)
					}

					message, err := r.add(packet)
					if err != nil {
						t.Fatalf("add(%d) error = %v", i, err)
					}

					if message != nil {
						if i != len(packets)-1 {
							t.Fatalf("the message ended at packet %d of %d", i, len(packets))
						}

						got = message
					}
				}

				if !bytes.Equal(got, payload) {
					t.Fatalf("reassembled %d bytes, want %d", len(got), len(payload))
				}
			})
		}
	}
}

// Two messages in a row are two messages, not one: the id in the header is what
// keeps a second message from being taken for the tail of the first.
func TestReassemblerKeepsMessagesApart(t *testing.T) {
	var r reassembler

	for _, text := range []string{"first", "second", "third"} {
		packets, err := fragment([]byte(text), 12, 3)
		if err != nil {
			t.Fatalf("fragment() error = %v", err)
		}

		var got []byte

		for _, packet := range packets {
			message, err := r.add(packet)
			if err != nil {
				t.Fatalf("add() error = %v", err)
			}

			if message != nil {
				got = message
			}
		}

		if string(got) != text {
			t.Fatalf("reassembled %q, want %q", got, text)
		}
	}
}

// A gap is unrecoverable, and the reassembler says so instead of handing up a
// message with a hole in it.
func TestReassemblerRefusesAGap(t *testing.T) {
	packets, err := fragment(bytes.Repeat([]byte("z"), 100), 20, 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	if len(packets) < 3 {
		t.Fatalf("the test needs at least three packets, got %d", len(packets))
	}

	var r reassembler

	if _, err := r.add(packets[0]); err != nil {
		t.Fatalf("the first packet was refused: %v", err)
	}

	if _, err := r.add(packets[2]); !errors.Is(err, ErrFragment) {
		t.Fatalf("skipping a packet gave %v, want ErrFragment", err)
	}
}

// A packet of one message cannot continue another.
func TestReassemblerRefusesAnotherMessagesPacket(t *testing.T) {
	first, err := fragment(bytes.Repeat([]byte("a"), 100), 20, 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	second, err := fragment(bytes.Repeat([]byte("b"), 100), 20, 2)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	var r reassembler

	if _, err := r.add(first[0]); err != nil {
		t.Fatalf("the first packet was refused: %v", err)
	}

	if _, err := r.add(second[1]); !errors.Is(err, ErrFragment) {
		t.Fatalf("a foreign packet gave %v, want ErrFragment", err)
	}
}

// A packet that starts with no message in progress must be the first packet of
// one: a stream that has lost its place is not repaired by guessing.
func TestReassemblerRefusesALaterPacketFirst(t *testing.T) {
	packets, err := fragment(bytes.Repeat([]byte("q"), 100), 20, 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	var r reassembler

	if _, err := r.add(packets[1]); !errors.Is(err, ErrFragment) {
		t.Fatalf("a second packet with no first gave %v, want ErrFragment", err)
	}
}

// A declared size beyond the limit is refused before anything is allocated: the
// peer chooses it, so it is checked before it is trusted.
func TestReassemblerRefusesAnOversizedMessage(t *testing.T) {
	packet := make([]byte, headerLen+lengthLen)
	packet[0], packet[1] = 0, 1 // id 1
	packet[4] = 0x01            // 16 MiB, big-endian
	packet[5] = 0x00
	packet[6] = 0x00
	packet[7] = 0x00

	var r reassembler

	if _, err := r.add(packet); !errors.Is(err, ErrMessage) {
		t.Fatalf("an oversized message gave %v, want ErrMessage", err)
	}
}

// A packet too short to carry the header, or a first packet with no length, is
// a bad message rather than a gap.
func TestReassemblerRefusesShortPackets(t *testing.T) {
	var r reassembler

	if _, err := r.add([]byte{0, 1}); !errors.Is(err, ErrMessage) {
		t.Fatalf("a 2-byte packet gave %v, want ErrMessage", err)
	}

	first := make([]byte, headerLen+lengthLen-1)
	if _, err := r.add(first); !errors.Is(err, ErrMessage) {
		t.Fatalf("a first packet without a length gave %v, want ErrMessage", err)
	}
}

// A packet size that cannot carry the framing is refused at the sender, where
// the transport is chosen, rather than producing packets nobody can read.
func TestFragmentRefusesAnImpossibleMTU(t *testing.T) {
	if _, err := fragment([]byte("hello"), headerLen+lengthLen, 1); !errors.Is(err, ErrMessage) {
		t.Fatalf("a packet of %d bytes gave %v, want ErrMessage", headerLen+lengthLen, err)
	}
}

// A message over the limit is refused rather than fragmented: the limit is what
// keeps a peer from making the receiver allocate arbitrarily.
func TestFragmentRefusesAnOversizedMessage(t *testing.T) {
	if _, err := fragment(make([]byte, MaxMessage+1), 20, 1); !errors.Is(err, ErrMessage) {
		t.Fatalf("an oversized payload gave %v, want ErrMessage", err)
	}
}

// itoa is strconv.Itoa without the import, so the table names read cleanly.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}
