package ui

import (
	"net"
	"strconv"
	"time"

	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
	"github.com/fiwon123/cthrone/internal/discovery"
)

const (
	// deviceTTL is how long a device stays in the list after it was last seen.
	// A host announces every two seconds, so this tolerates several missed
	// announcements while a stopped host still leaves within a few seconds.
	deviceTTL = discovery.BeaconTTL

	// scanDialTimeout is how long one liveness check may take. The scanner
	// spends the same budget on its own probes.
	scanDialTimeout = 300 * time.Millisecond

	// pruneInterval is how often the list is swept for devices that went quiet.
	pruneInterval = 5 * time.Second
)

// device is one row of the Devices list: the last beacon heard from it and
// when that was, or — for a device the radio found — what the scan reported and
// when it ran.
type device struct {
	discovery.Beacon

	// Radio marks a row a Bluetooth scan found. Such a row is not a network
	// device: it has no port to dial, its address is a radio address, and it is
	// reached by the Bluetooth transport rather than over TCP. The flag keeps
	// the two kinds apart in one list, because a scan of one does not disprove
	// a device found by the other.
	Radio bool

	seen time.Time
}

// dialFunc reports whether addr accepts a TCP connection. It is a parameter
// rather than a direct call so the tests never touch the network.
type dialFunc func(addr string, timeout time.Duration) error

// dialDevice is the real liveness check: a bare TCP connect, closed at once.
func dialDevice(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}

	return conn.Close()
}

// deviceKey identifies a device the way the list dedupes them.
func deviceKey(d device) string {
	if d.Radio {
		// A radio address has no port, and its own namespace: two devices on
		// different carriers cannot be the same device, and a MAC address that
		// happened to read like an IP would collide without the prefix.
		return "bluetooth:" + d.Addr
	}

	return d.Addr + ":" + strconv.Itoa(d.Port)
}

// localAddrs is every address this device holds. It is a variable so a test
// supplies the addresses instead of asking the machine.
var localAddrs = discovery.LocalAddrs

// defaultRouteIP is the address this device's default route uses: the network a
// peer on this LAN would find it on. It is a variable for the same reason —
// a test supplies the answer instead of reading the machine's routing table.
var defaultRouteIP = discovery.DefaultRouteIP

// isSelf reports whether addr is this device. A host answers on its own
// addresses, and hears its own announcement when the operating system loops
// the broadcast back; neither is a device to connect to.
func isSelf(addr string, own []string) bool {
	for _, a := range own {
		if a == addr {
			return true
		}
	}

	return false
}

// deviceLabel is how one row of the Devices list reads: the address the
// connect uses, with the device name in front of it when one has been heard.
//
// A device with no port is a radio device: its address is a Bluetooth address,
// which has no port to print. The name goes in front there for the same reason
// it does on the network — two phones may be called the same thing — and the
// address is what the connect needs, so both stay on the line.
func deviceLabel(b discovery.Beacon) string {
	addr := b.Addr
	if b.Port > 0 {
		addr = b.Addr + ":" + strconv.Itoa(b.Port)
	}

	if b.Name == "" {
		return addr
	}

	return b.Name + " — " + addr
}

// upsertDevice records a beacon heard at now. A device already listed takes
// its new state instead of appearing twice.
func upsertDevice(list []device, b discovery.Beacon, now time.Time) []device {
	for i := range list {
		if list[i].Radio {
			continue
		}

		if list[i].Addr == b.Addr && list[i].Port == b.Port {
			// A scan finds an address, not a name: it must not erase the one a
			// beacon announced, or pressing Scan would make the list forget
			// who the device is.
			if b.Name == "" {
				b.Name = list[i].Name
			}

			if b.Protocol == 0 {
				b.Protocol = list[i].Protocol
			}

			list[i].Beacon = b
			list[i].seen = now

			return list
		}
	}

	return append(list, device{Beacon: b, seen: now})
}

// upsertPeer records a device a radio scan found. A peer is replaced by the next
// scan that sees it, so a device that is still there does not appear twice and
// one that has left keeps the row until a scan says otherwise.
func upsertPeer(list []device, p bluetoothcore.Peer, now time.Time) []device {
	for i := range list {
		if !list[i].Radio || list[i].Addr != p.Address {
			continue
		}

		list[i].Name = p.Name
		list[i].seen = now

		return list
	}

	return append(list, device{
		Beacon: discovery.Beacon{Addr: p.Address, Name: p.Name},
		Radio:  true,
		seen:   now,
	})
}

// pruneDevices drops the devices not heard for deviceTTL.
//
// A radio row is exempt: it was never announced, so there is no interval to
// miss. It is a snapshot of one scan, and the next scan is what replaces it.
func pruneDevices(list []device, now time.Time) []device {
	out := list[:0]
	for _, d := range list {
		if d.Radio || now.Sub(d.seen) <= deviceTTL {
			out = append(out, d)
		}
	}

	// The dropped rows still sit in the backing array; clear them so a stale
	// beacon is not reachable through a reslice.
	clear(list[len(out):])

	return out
}

// visibleDevices is the rows the Devices list shows for the transport in force.
// The list holds both kinds at once — a scan of one does not disprove a device
// found by the other — but a row from the other carrier is not a peer the
// current Connect can reach, so it is not offered.
func visibleDevices(list []device, mode transportMode) []device {
	radio := mode == transportBluetooth

	out := make([]device, 0, len(list))

	for _, d := range list {
		if d.Radio == radio {
			out = append(out, d)
		}
	}

	return out
}

// scanHint is the line beside the Scan button: it says what the button will
// look for, which is the one thing the two scans do not share.
func scanHint(mode transportMode) string {
	if mode == transportBluetooth {
		return "Bluetooth devices in range — about ten metres"
	}

	return "UDP beacons on port 9999"
}

// scanButtonText is the same distinction on the button itself: a user who reads
// only the button still knows which carrier it will walk.
func scanButtonText(mode transportMode) string {
	if mode == transportBluetooth {
		return "Scan Bluetooth"
	}

	return "Scan network"
}

// addScanned lists the addresses a port scan found alive. A scan knows the
// address and nothing else, so the name stays empty until a beacon fills it in.
func addScanned(list []device, addrs []string, port int, now time.Time) []device {
	for _, addr := range addrs {
		list = upsertDevice(list, discovery.Beacon{Addr: addr, Port: port}, now)
	}

	return list
}

// checkDevices reports which of the listed devices still answer, keyed by
// deviceKey. A host that was stopped since it was last heard does not.
func checkDevices(list []device, dial dialFunc, timeout time.Duration) map[string]bool {
	answered := make(map[string]bool, len(list))
	for _, d := range list {
		addr := net.JoinHostPort(d.Addr, strconv.Itoa(d.Port))
		if dial(addr, timeout) == nil {
			answered[deviceKey(d)] = true
		}
	}

	return answered
}

// withoutDead drops the rows in current that were checked and did not answer.
// A row discovered while the check was running was not in the snapshot, so it
// is kept: nothing has disproved it yet.
func withoutDead(current, checked []device, answered map[string]bool) []device {
	wasChecked := make(map[string]bool, len(checked))
	for _, d := range checked {
		wasChecked[deviceKey(d)] = true
	}

	out := current[:0]
	for _, d := range current {
		key := deviceKey(d)
		if wasChecked[key] && !answered[key] {
			continue
		}
		out = append(out, d)
	}

	clear(current[len(out):])

	return out
}
