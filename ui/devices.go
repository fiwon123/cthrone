package ui

import (
	"net"
	"strconv"
	"time"

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
// when that was.
type device struct {
	discovery.Beacon
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
	return d.Addr + ":" + strconv.Itoa(d.Port)
}

// upsertDevice records a beacon heard at now. A device already listed takes
// its new state instead of appearing twice.
func upsertDevice(list []device, b discovery.Beacon, now time.Time) []device {
	for i := range list {
		if list[i].Addr == b.Addr && list[i].Port == b.Port {
			list[i].Beacon = b
			list[i].seen = now

			return list
		}
	}

	return append(list, device{Beacon: b, seen: now})
}

// pruneDevices drops the devices not heard for deviceTTL.
func pruneDevices(list []device, now time.Time) []device {
	out := list[:0]
	for _, d := range list {
		if now.Sub(d.seen) <= deviceTTL {
			out = append(out, d)
		}
	}

	// The dropped rows still sit in the backing array; clear them so a stale
	// beacon is not reachable through a reslice.
	clear(list[len(out):])

	return out
}

// addScanned lists the addresses a port scan found alive.
func addScanned(list []device, addrs []string, port int, now time.Time) []device {
	for _, addr := range addrs {
		list = upsertDevice(list, discovery.Beacon{Name: addr, Addr: addr, Port: port}, now)
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
