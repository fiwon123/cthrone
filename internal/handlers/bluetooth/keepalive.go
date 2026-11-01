package bluetooth

import (
	"encoding/binary"
	"sync"
	"time"
)

// A BLE link reports almost nothing. BlueZ on Linux hands a peripheral no
// connect or disconnect event, and a notification written with no subscriber
// attached does not fail — it only sets a property. So without a probe, a host
// would go on believing a device that walked out of range is still in the room,
// and the next device would be refused because the old one never left.
//
// The probe is a control packet on the same stream, with a reserved message id:
// every packet is inspected before the session's reassembler sees it, so a
// control packet never reaches the layer above. Silence for readTimeout ends
// the link, which is what makes a departure observable.
//
// This mirrors the websocket keepalive (#193) on purpose: the two transports
// answer the same question the same way, and both keep their intervals in
// variables so a test can watch the mechanism without waiting 70 seconds.
const (
	// controlID is the message id of a packet that belongs to the link rather
	// than the conversation. A data message never uses it: fragment numbers
	// start at zero and are incremented per send, so reaching this value would
	// take 65,535 messages.
	controlID = 0xFFFF

	// The control packet is [id:2][index:2][kind:1]. It has no declared length
	// because it never reaches the reassembler.
	controlPing byte = 'P'
	controlPong byte = 'O'

	controlLen = 5
)

// pingInterval is how often an idle side probes the link; readTimeout is how
// long it waits for anything — data or a probe — before it declares the link
// gone. readTimeout is several intervals, so one lost packet is not a
// disconnect.
//
// They are fixed rather than configurable, and newKeepalive takes them as
// arguments so a test can run the mechanism in milliseconds without touching
// package state.
const (
	pingInterval = 20 * time.Second
	readTimeout  = 70 * time.Second
)

// keepaliveLink wraps a Link with the probe. It is the Link the session uses:
// data passes through untouched, control packets are answered and swallowed,
// and a link that has gone quiet is ended with a reason.
type keepaliveLink struct {
	inner Link

	ping    time.Duration
	timeout time.Duration

	out  chan []byte
	done chan struct{}
	once sync.Once

	mu     sync.Mutex
	reason string
}

// WithKeepalive adds the probe to inner and returns the link to hand to New.
//
// It is what a transport wraps a raw link with: without it a host never learns
// that a device left, because a BLE peripheral is told nothing about
// disconnections.
func WithKeepalive(inner Link) Link {
	return newKeepalive(inner, pingInterval, readTimeout)
}

// newKeepalive is WithKeepalive with the intervals named by the caller: a test
// runs the probe in milliseconds instead of minutes.
func newKeepalive(inner Link, ping, timeout time.Duration) *keepaliveLink {
	k := &keepaliveLink{
		inner:   inner,
		ping:    ping,
		timeout: timeout,
		out:     make(chan []byte, 64),
		done:    make(chan struct{}),
	}

	go k.run()

	return k
}

func (k *keepaliveLink) MTU() int { return k.inner.MTU() }

// Name forwards the transport's own name, so the first line a session shows
// still says which device it is talking to.
func (k *keepaliveLink) Name() string {
	if named, ok := k.inner.(interface{ Name() string }); ok {
		return named.Name()
	}

	return ""
}

func (k *keepaliveLink) Incoming() <-chan []byte { return k.out }

func (k *keepaliveLink) Write(packet []byte) error { return k.inner.Write(packet) }

func (k *keepaliveLink) Close(reason string) error {
	k.end(reason)

	return nil
}

func (k *keepaliveLink) Reason() string {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.reason != "" {
		return k.reason
	}

	return k.inner.Reason()
}

// run moves packets up, answers probes, and ends the link when it goes quiet.
// It owns k.out: the channel closes when it returns, which is what tells the
// session the link has ended.
func (k *keepaliveLink) run() {
	defer close(k.out)

	ticker := time.NewTicker(k.ping)
	defer ticker.Stop()

	timer := time.NewTimer(k.timeout)
	defer timer.Stop()

	in := k.inner.Incoming()

	for {
		select {
		case packet, ok := <-in:
			if !ok {
				// The link ended underneath us: the session reads the reason
				// from Reason(), which prefers the inner link's.
				return
			}

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			timer.Reset(k.timeout)

			if len(packet) >= 2 && binary.BigEndian.Uint16(packet[:2]) == controlID {
				if len(packet) == controlLen && packet[4] == controlPing {
					// A peer that probes is alive; answering is what keeps its
					// side of the link open.
					if err := k.sendControl(controlPong); err != nil {
						k.end("bluetooth: the link stopped accepting packets")

						return
					}
				}

				continue
			}

			select {
			case k.out <- packet:
			case <-k.done:
				return
			}

		case <-ticker.C:
			if err := k.sendControl(controlPing); err != nil {
				k.end("bluetooth: the link stopped accepting packets")

				return
			}

		case <-timer.C:
			k.end("bluetooth: nothing has arrived for " + k.timeout.String() + "; the device is gone")

			return

		case <-k.done:
			return
		}
	}
}

// sendControl writes one control packet.
func (k *keepaliveLink) sendControl(kind byte) error {
	if k.inner.MTU() < controlLen {
		// A link this small cannot carry the probe. The link still works; it
		// simply relies on the peer's own probe, which reads a silence it
		// cannot break either. No radio in use is this small.
		return nil
	}

	packet := make([]byte, controlLen)
	binary.BigEndian.PutUint16(packet[0:], controlID)
	binary.BigEndian.PutUint16(packet[2:], 0)
	packet[4] = kind

	return k.inner.Write(packet)
}

// end records why the link is over and closes it once.
func (k *keepaliveLink) end(reason string) {
	k.once.Do(func() {
		k.mu.Lock()
		k.reason = reason
		k.mu.Unlock()

		close(k.done)

		_ = k.inner.Close(reason)
	})
}
