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
	if list[0].Port != 8080 || list[0].Name != "192.168.1.20" {
		t.Fatalf("row = %+v, want the scanned address on the scanned port", list[0])
	}
}
