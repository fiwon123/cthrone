// Package bluetooth is the chat transport that needs no network and no broker.
//
// Bluetooth is the one carrier that works when the network itself is the
// problem: a phone with no Wi-Fi, two laptops on a network that isolates its
// clients. It is a different kind of link from the other two, and the difference
// is why this package exists:
//
//   - BLE is packet-oriented. One notification carries at most a few dozen
//     bytes, while the wire envelope packs a file chunk of 256 KiB. So a message
//     is fragmented into packets here, on the way in and out, and reassembled
//     before the layer above sees it.
//   - A BLE link reports almost nothing. There is no connect or disconnect
//     event on a Linux peripheral and no copy of the sender on a write, so the
//     host serves one device at a time and a new device is accepted only after
//     the previous link has ended.
//
// Everything above this package is unchanged: a Session here is a
// session.Session like the websocket's or the NATS one, internal/e2e seals it
// with the device key, and internal/chat runs the same loop over it. Bluetooth
// is a carrier, not a fourth security model — the peer still has to prove its
// key before a payload crosses.
//
// The GATT implementation is Linux (BlueZ over D-Bus) and Windows (WinRT). The framing and the
// session are portable and are tested without a radio; core/bluetooth says what
// the other platforms do.
package bluetooth

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// headerLen is what each packet carries beside its bytes: which message it
	// belongs to, and which packet of that message it is. Both are 16-bit, so
	// a message of 512 KiB at 20 bytes a packet is 26,215 fragments — well
	// inside the count.
	headerLen = 4

	// lengthLen is the declared total in the first packet of a message, so a
	// receiver knows when it is complete and can refuse an absurd size before
	// allocating anything.
	lengthLen = 4

	// MaxMessage bounds one reassembled message. A file chunk is 256 KiB and
	// the sealed envelope adds a few dozen bytes, so this leaves room for the
	// largest frame the wire can produce and refuses anything beyond it.
	MaxMessage = 512 << 10
)

// Errors the caller turns into a closed session. A packet stream that has lost
// its place cannot be repaired: the bytes after a gap belong to a message the
// receiver will never complete, so the session ends and the user reconnects.
var (
	// ErrFragment is a packet out of sequence: a gap, a duplicate, or a packet
	// of a message that is no longer being assembled.
	ErrFragment = errors.New("bluetooth: packet out of sequence")

	// ErrMessage is a message larger than MaxMessage, or an empty packet.
	ErrMessage = errors.New("bluetooth: bad message size")
)

// fragment splits payload into packets no larger than mtu bytes.
//
// Every packet begins with the message id and the packet index, so a receiver
// can tell a fresh message from the rest of the one before it and notice a
// packet that never arrived. The first packet also carries the total length,
// which is what ends the message: a partial message is never handed up.
func fragment(payload []byte, mtu int, id uint16) ([][]byte, error) {
	if mtu <= headerLen+lengthLen {
		return nil, fmt.Errorf("%w: a packet of %d bytes carries no payload", ErrMessage, mtu)
	}

	if len(payload) > MaxMessage {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrMessage, len(payload), MaxMessage)
	}

	room := mtu - headerLen // bytes for the payload of one packet
	first := room - lengthLen

	packets := make([][]byte, 0, 1+(len(payload)+first-1)/room)

	// The first packet: id, index 0, the total length, then as much of the
	// payload as fits beside them.
	head := make([]byte, headerLen+lengthLen+min(first, len(payload)))
	binary.BigEndian.PutUint16(head[0:], id)
	binary.BigEndian.PutUint16(head[2:], 0)
	binary.BigEndian.PutUint32(head[headerLen:], uint32(len(payload)))
	copied := copy(head[headerLen+lengthLen:], payload[:min(first, len(payload))])
	packets = append(packets, head[:headerLen+lengthLen+copied])

	for offset, index := first, uint16(1); offset < len(payload); index++ {
		n := min(room, len(payload)-offset)

		packet := make([]byte, headerLen+n)
		binary.BigEndian.PutUint16(packet[0:], id)
		binary.BigEndian.PutUint16(packet[2:], index)
		copy(packet[headerLen:], payload[offset:offset+n])

		packets = append(packets, packet)

		offset += n
	}

	return packets, nil
}

// reassembler turns the packets of one link back into messages.
//
// It is not safe for concurrent use: one link delivers its packets in order on
// one goroutine, and that is the only access it has.
type reassembler struct {
	active bool
	id     uint16
	next   uint16
	want   int
	buf    []byte
}

// add feeds one packet in. It returns the message when that packet completed
// one, and nil while the message is still incomplete.
func (r *reassembler) add(packet []byte) ([]byte, error) {
	if len(packet) < headerLen {
		return nil, fmt.Errorf("%w: %d bytes, shorter than the header", ErrMessage, len(packet))
	}

	id := binary.BigEndian.Uint16(packet[0:])
	index := binary.BigEndian.Uint16(packet[2:])
	body := packet[headerLen:]

	if !r.active {
		if index != 0 {
			return nil, fmt.Errorf("%w: packet %d starts no message", ErrFragment, index)
		}

		if len(body) < lengthLen {
			return nil, fmt.Errorf("%w: the first packet carries %d bytes, without a length", ErrMessage, len(body))
		}

		total := binary.BigEndian.Uint32(body[:lengthLen])
		if total > MaxMessage {
			return nil, fmt.Errorf("%w: the message declares %d bytes, the limit is %d", ErrMessage, total, MaxMessage)
		}

		r.active = true
		r.id = id
		r.next = 1
		r.want = int(total)
		r.buf = append(r.buf[:0], body[lengthLen:]...)

		return r.take()
	}

	if id != r.id || index != r.next {
		return nil, fmt.Errorf("%w: packet %d of message %d, want %d of %d", ErrFragment, index, id, r.next, r.id)
	}

	r.buf = append(r.buf, body...)
	r.next++

	return r.take()
}

// take returns the message once every byte it declared has arrived, and resets
// for the next one.
func (r *reassembler) take() ([]byte, error) {
	if len(r.buf) < r.want {
		return nil, nil
	}

	if len(r.buf) > r.want {
		r.reset()

		return nil, fmt.Errorf("%w: %d bytes arrived for a message of %d", ErrMessage, len(r.buf), r.want)
	}

	message := make([]byte, len(r.buf))
	copy(message, r.buf)
	r.reset()

	return message, nil
}

func (r *reassembler) reset() {
	r.active = false
	r.id = 0
	r.next = 0
	r.want = 0
	r.buf = r.buf[:0]
}
