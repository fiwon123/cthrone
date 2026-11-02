package discovery

import (
	"net"
	"testing"
	"time"
)

// FuzzBeacon drives the whole path a datagram takes on a listener: the parse,
// the room it is recorded in, the eviction when the room is full, and the prune.
//
// It is the one input in this program that needs no credential at all: anybody
// on the segment can send it, and the listener goroutine throws away a panic
// with a bare recover — which would end discovery in that process silently
// (#414). A panic here is the finding; an allocation that never ends is the
// other one, which is why the packet stays near the 4096 bytes the listener
// reads.
func FuzzBeacon(f *testing.F) {
	f.Add([]byte(`{"name":"laptop","port":8080,"protocol":1}`))
	f.Add([]byte(`{"name":"","port":0,"protocol":0,"addr":"192.168.1.7"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"port":-1}`))
	f.Add([]byte(`{"name":"` + string(make([]byte, 64)) + `","port":65536}`))

	from := net.IPv4(192, 168, 1, 20)

	f.Fuzz(func(t *testing.T, packet []byte) {
		now := time.Unix(1_700_000_000, 0)

		listener := NewListener()

		// The listener's own clock, so Found and Prune agree with what was
		// observed: they read time.Now otherwise, and every entry would look
		// ancient.
		listener.now = func() time.Time { return now }

		beacon, ok := parseBeacon(packet, from)
		if !ok {
			return
		}

		listener.observe(beacon, now)

		// The same packet twice must not grow the room: a device heard twice is
		// one device.
		listener.observe(beacon, now)

		// A second address is a second device; the key is built from the
		// address and the port, and neither comes with a bound.
		other := beacon
		other.Addr = from.String() + ".1"
		listener.observe(other, now)

		if got := len(listener.Found()); got == 0 {
			t.Fatal("a parsed beacon was not recorded")
		}

		listener.Prune(now.Add(BeaconTTL+time.Second), BeaconTTL)

		if got := len(listener.Found()); got != 0 {
			t.Errorf("%d beacons survived their own TTL", got)
		}
	})
}

// The parse fills the address from the sender only when the packet carries
// none: a beacon that names an address is the one being described, and the
// sender's address is only a fallback.
func TestParseBeaconUsesTheSenderOnlyAsAFallback(t *testing.T) {
	from := net.IPv4(192, 168, 1, 20)

	named, ok := parseBeacon([]byte(`{"name":"laptop","port":8080,"addr":"10.0.0.5"}`), from)
	if !ok || named.Addr != "10.0.0.5" {
		t.Errorf("a named address became %q, want the one in the packet", named.Addr)
	}

	unnamed, ok := parseBeacon([]byte(`{"name":"laptop","port":8080}`), from)
	if !ok || unnamed.Addr != from.String() {
		t.Errorf("an unnamed beacon became %q, want the sender %q", unnamed.Addr, from.String())
	}

	if _, ok := parseBeacon([]byte(`nope`), from); ok {
		t.Error("a packet that is not JSON parsed as a beacon")
	}
}
