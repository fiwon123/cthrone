package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
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
		// A radio device has no port: the address is a Bluetooth address, and
		// printing an empty one after a colon would be a port that never was.
		{"radio, named", discovery.Beacon{Name: "phone", Addr: "AA:BB:CC:DD:EE:01"}, "phone — AA:BB:CC:DD:EE:01"},
		{"radio, unnamed", discovery.Beacon{Addr: "AA:BB:CC:DD:EE:02"}, "AA:BB:CC:DD:EE:02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceLabel(tt.beacon); got != tt.want {
				t.Errorf("deviceLabel(%+v) = %q, want %q", tt.beacon, got, tt.want)
			}
		})
	}
}

// radioRow is one device a Bluetooth scan found.
func radioRow(name, addr string, seen time.Time) device {
	return device{Beacon: discovery.Beacon{Name: name, Addr: addr}, Radio: true, seen: seen}
}

// A radio scan reports the same device once however often it advertises, and
// updates it when the name or the signal changes.
func TestUpsertPeerKeepsOneRowPerAddress(t *testing.T) {
	list := upsertPeer(nil, bluetoothcore.Peer{Address: "AA:BB:CC:DD:EE:01", Name: "phone"}, deviceTime)
	list = upsertPeer(list, bluetoothcore.Peer{Address: "AA:BB:CC:DD:EE:01", Name: "phone-renamed"}, deviceTime.Add(2*time.Second))

	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}

	if !list[0].Radio {
		t.Error("the row is not marked as a radio device")
	}

	if list[0].Name != "phone-renamed" {
		t.Errorf("name = %q, want the latest scan", list[0].Name)
	}

	if !list[0].seen.Equal(deviceTime.Add(2 * time.Second)) {
		t.Errorf("seen = %v, want the latest scan", list[0].seen)
	}
}

// A beacon and a radio device never share a row, even if their addresses read
// alike: they are different carriers, and only one of them can be dialled.
func TestAPeerAndAReachableDeviceStayTwoRows(t *testing.T) {
	list := upsertDevice(nil, discovery.Beacon{Addr: "AA:BB:CC:DD:EE:01", Port: 8080}, deviceTime)
	list = upsertPeer(list, bluetoothcore.Peer{Address: "AA:BB:CC:DD:EE:01"}, deviceTime)

	if len(list) != 2 {
		t.Fatalf("the list holds %d rows, want one per carrier", len(list))
	}

	// And the network beacon does not take over the radio row's place.
	if list[0].Radio == list[1].Radio {
		t.Fatalf("both rows are the same carrier: %+v", list)
	}
}

// A radio row was never announced, so there is no beacon interval to miss: it
// stays until the next scan replaces it.
func TestPruneDevicesKeepsRadioRows(t *testing.T) {
	list := []device{radioRow("phone", "AA:BB:CC:DD:EE:01", deviceTime)}

	got := pruneDevices(list, deviceTime.Add(deviceTTL+time.Hour))
	if len(got) != 1 {
		t.Fatalf("pruneDevices dropped a radio row: %+v", got)
	}
}

// The Devices tab shows the carrier in force: a row from the other transport is
// not a peer the Connect button can reach.
func TestVisibleDevicesFollowsTheTransport(t *testing.T) {
	list := []device{
		listed("laptop", "192.168.1.20", 8080, deviceTime),
		radioRow("phone", "AA:BB:CC:DD:EE:01", deviceTime),
	}

	for _, tt := range []struct {
		mode transportMode
		want string
	}{
		{transportWebsocket, "laptop"},
		{transportNATS, "laptop"},
		{transportBluetooth, "phone"},
	} {
		got := visibleDevices(list, tt.mode)
		if len(got) != 1 || got[0].Name != tt.want {
			t.Errorf("visibleDevices in mode %v = %+v, want only %q", tt.mode, got, tt.want)
		}
	}

	// The full list is untouched: filtering is a view, not a prune.
	if len(list) != 2 {
		t.Fatalf("filtering the list changed it: %+v", list)
	}
}

// The scan button and its hint say which carrier they will walk, because the
// two scans look for entirely different things.
func TestScanWordingFollowsTheTransport(t *testing.T) {
	if hint := scanHint(transportBluetooth); !strings.Contains(hint, "Bluetooth") {
		t.Errorf("the Bluetooth hint is %q", hint)
	}

	if hint := scanHint(transportWebsocket); !strings.Contains(hint, "beacons") {
		t.Errorf("the network hint is %q", hint)
	}

	if text := scanButtonText(transportBluetooth); text == scanButtonText(transportWebsocket) {
		t.Errorf("the two scans share the button label %q", text)
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
