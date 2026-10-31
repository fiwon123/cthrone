package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
)

var deviceTime = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func listed(name, addr string, port int, seen time.Time) device {
	return device{Beacon: discovery.Beacon{Name: name, Addr: addr, Port: port}, seen: seen}
}

func TestUpsertDeviceKeepsOneRowPerAddress(t *testing.T) {
	list := upsertDevice(nil, discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}, deviceTime)
	list = upsertDevice(list, discovery.Beacon{Name: "laptop-renamed", Addr: "192.168.1.20", Port: 8080}, deviceTime.Add(2*time.Second))
	list = upsertDevice(list, discovery.Beacon{Name: "other", Addr: "192.168.1.21", Port: 8080}, deviceTime)

	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].Name != "laptop-renamed" {
		t.Fatalf("name = %q, want the latest hearing", list[0].Name)
	}
	if !list[0].seen.Equal(deviceTime.Add(2 * time.Second)) {
		t.Fatalf("seen = %v, want the latest hearing", list[0].seen)
	}
}

func TestPruneDevicesDropsQuietRows(t *testing.T) {
	list := []device{
		listed("old", "192.168.1.20", 8080, deviceTime),
		listed("new", "192.168.1.21", 8080, deviceTime.Add(10*time.Second)),
	}

	got := pruneDevices(list, deviceTime.Add(deviceTTL+time.Second))
	if len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("pruneDevices = %+v, want only the recently heard device", got)
	}
}

func TestPruneDevicesKeepsFreshRows(t *testing.T) {
	list := []device{listed("fresh", "192.168.1.20", 8080, deviceTime)}

	got := pruneDevices(list, deviceTime.Add(deviceTTL))
	if len(got) != 1 {
		t.Fatalf("pruneDevices at exactly the TTL = %+v, want the device kept", got)
	}
}

func TestCheckDevicesReportsWhatAnswers(t *testing.T) {
	list := []device{
		listed("alive", "192.168.1.20", 8080, deviceTime),
		listed("stopped", "192.168.1.21", 8080, deviceTime),
	}

	dial := func(addr string, timeout time.Duration) error {
		if addr == "192.168.1.21:8080" {
			return errors.New("connection refused")
		}

		return nil
	}

	answered := checkDevices(list, dial, scanDialTimeout)
	if !answered["192.168.1.20:8080"] {
		t.Fatal("the answering device was not reported")
	}
	if answered["192.168.1.21:8080"] {
		t.Fatal("a refused device was reported as answering")
	}
}

func TestWithoutDeadDropsOnlyCheckedFailures(t *testing.T) {
	checked := []device{
		listed("stopped", "192.168.1.20", 8080, deviceTime),
		listed("alive", "192.168.1.21", 8080, deviceTime),
	}
	answered := map[string]bool{"192.168.1.21:8080": true}

	current := append([]device(nil), checked...)
	// A beacon that arrived while the check was running was never checked.
	current = append(current, listed("late", "192.168.1.22", 8080, deviceTime))

	got := withoutDead(current, checked, answered)
	if len(got) != 2 {
		t.Fatalf("withoutDead = %+v, want the alive and the late device", got)
	}
	for _, d := range got {
		if d.Name == "stopped" {
			t.Fatal("a device that did not answer was kept")
		}
	}
}

func TestAddScannedListsNewAddresses(t *testing.T) {
	list := addScanned(nil, []string{"192.168.1.20", "192.168.1.21"}, 8080, deviceTime)
	list = addScanned(list, []string{"192.168.1.20"}, 8080, deviceTime.Add(time.Second))

	if len(list) != 2 {
		t.Fatalf("len = %d, want 2 — a rescan must not duplicate", len(list))
	}
	if list[0].Port != 8080 || list[0].Addr != "192.168.1.20" {
		t.Fatalf("row = %+v, want the scanned address on the scanned port", list[0])
	}
	if list[0].Name != "" {
		t.Fatalf("name = %q, want no name: a scan finds an address, not a device name", list[0].Name)
	}
}

// A scan must not erase what a beacon already told us. The row used to end up
// with the address in the name field, so the list printed the address twice
// and the device lost the name it had.
func TestScanKeepsTheNameABeaconAnnounced(t *testing.T) {
	list := upsertDevice(nil, discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080, Protocol: 1}, deviceTime)
	list = addScanned(list, []string{"192.168.1.20"}, 8080, deviceTime.Add(time.Second))

	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].Name != "laptop" {
		t.Fatalf("name = %q, want %q: a scan knows no name and must not overwrite one", list[0].Name, "laptop")
	}
	if list[0].Protocol != 1 {
		t.Fatalf("protocol = %d, want the announced one kept", list[0].Protocol)
	}
	if !list[0].seen.Equal(deviceTime.Add(time.Second)) {
		t.Fatal("the scan did not refresh when the device was last seen")
	}
}

// The other order: the scan finds the host first, and the announcement that
// follows names it.
func TestABeaconNamesAScannedRow(t *testing.T) {
	list := addScanned(nil, []string{"192.168.1.21"}, 8080, deviceTime)
	list = upsertDevice(list, discovery.Beacon{Name: "phone", Addr: "192.168.1.21", Port: 8080}, deviceTime.Add(time.Second))

	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].Name != "phone" {
		t.Fatalf("name = %q, want the announced name", list[0].Name)
	}
}

// The row shows the name beside the address, and an unnamed device shows its
// address once — never twice.
func TestDeviceLabel(t *testing.T) {
	tests := []struct {
		name   string
		beacon discovery.Beacon
		want   string
	}{
		{"named", discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}, "laptop — 192.168.1.20:8080"},
		{"unnamed", discovery.Beacon{Addr: "192.168.1.21", Port: 8080}, "192.168.1.21:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceLabel(tt.beacon); got != tt.want {
				t.Errorf("deviceLabel(%+v) = %q, want %q", tt.beacon, got, tt.want)
			}
		})
	}
}

// A host answers on its own addresses and hears its own beacon when the
// operating system loops the broadcast back. Neither is a device to connect to,
// so every address the machine holds is filtered — not only the first one.
func TestIsSelf(t *testing.T) {
	own := []string{"192.168.1.20", "10.0.5.7"}

	tests := []struct {
		name string
		addr string
		own  []string
		want bool
	}{
		{"first own address", "192.168.1.20", own, true},
		{"second own address", "10.0.5.7", own, true},
		{"another device", "192.168.1.21", own, false},
		{"empty address", "", own, false},
		{"no addresses known", "192.168.1.20", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelf(tt.addr, tt.own); got != tt.want {
				t.Errorf("isSelf(%q, %v) = %v, want %v", tt.addr, tt.own, got, tt.want)
			}
		})
	}
}
