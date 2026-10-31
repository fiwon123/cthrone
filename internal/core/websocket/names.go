package websocketcore

import (
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// BeaconWindow is how long a scan listens for announcements before it names
// what it found. A host announces every two seconds, so this hears one that
// started before the scan did; a host that is not announcing stays unnamed.
const BeaconWindow = 3 * time.Second

// beaconSource is the part of discovery.Listener a scan uses. It is an
// interface so a test supplies the beacons instead of binding the discovery
// port — a unit test must not need the network.
type beaconSource interface {
	Start()
	Found() []discovery.Beacon
}

// newBeaconSource is a variable for the same reason: the test replaces it.
var newBeaconSource = func() beaconSource { return discovery.NewListener() }

// BeaconNames names the addresses a scan found, from the announcements heard in
// window. The result is keyed by address.
//
// An address nobody announced is absent from the result, and that is a real
// answer rather than a failure: a host can be reachable over TCP while its
// broadcast is blocked, and a scan must not open a session to ask for a name —
// a websocket host asks a person before an unknown device joins, so every probe
// would be a prompt on the other screen.
//
// The listener has no Stop: it holds the discovery port until the process ends.
// That suits its one caller, the scan command, which is one scan per process; a
// long-lived caller should own a Listener instead of calling this per scan.
func BeaconNames(addrs []string, port int, window time.Duration) map[string]string {
	names := make(map[string]string, len(addrs))

	if len(addrs) == 0 || window <= 0 {
		return names
	}

	want := make(map[string]bool, len(addrs))
	for _, addr := range addrs {
		want[addr] = true
	}

	source := newBeaconSource()
	if source == nil {
		return names
	}

	source.Start()
	time.Sleep(window)

	// Found, not a callback: the listener collects during the window and the
	// scan reads once, so no beacon can arrive while the map is being built.
	for _, b := range source.Found() {
		if b.Port != port || b.Name == "" || !want[b.Addr] {
			continue
		}

		names[b.Addr] = b.Name
	}

	return names
}
