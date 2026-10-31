package discovery

import (
	"sync/atomic"
	"testing"
	"time"
)

var beaconTime = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestListenerPruneExpiresQuietDevices(t *testing.T) {
	l := NewListener()

	l.observe(Beacon{Name: "old", Addr: "192.168.1.20", Port: 8080}, beaconTime)
	l.observe(Beacon{Name: "new", Addr: "192.168.1.21", Port: 8080}, beaconTime.Add(8*time.Second))

	got := l.Prune(beaconTime.Add(13*time.Second), BeaconTTL)
	if len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("Prune = %+v, want only the device heard 5s ago", got)
	}

	// Pruning is not only a filter: the expired entry is gone for good, so a
	// later prune cannot bring it back.
	if got := l.Prune(beaconTime.Add(14*time.Second), BeaconTTL); len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("Prune after expiry = %+v, want the fresh device only", got)
	}
}

func TestListenerFoundExpires(t *testing.T) {
	l := NewListener()
	l.now = func() time.Time { return beaconTime }
	l.observe(Beacon{Name: "gone", Addr: "192.168.1.20", Port: 8080}, beaconTime)

	if got := l.Found(); len(got) != 1 {
		t.Fatalf("Found = %+v, want the device just heard", got)
	}

	l.now = func() time.Time { return beaconTime.Add(BeaconTTL + time.Second) }
	if got := l.Found(); len(got) != 0 {
		t.Fatalf("Found after BeaconTTL = %+v, want none", got)
	}
}

func TestListenerObserveKeepsOneEntryPerAddress(t *testing.T) {
	l := NewListener()
	b := Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}

	l.observe(b, beaconTime)
	b.Name = "laptop-renamed"
	l.observe(b, beaconTime)
	// The same address on another port is another service, so another device.
	l.observe(Beacon{Name: "other", Addr: "192.168.1.20", Port: 9090}, beaconTime)

	got := l.Prune(beaconTime, BeaconTTL)
	if len(got) != 2 {
		t.Fatalf("Prune = %+v, want two distinct devices", got)
	}

	for _, beacon := range got {
		if beacon.Port == 8080 && beacon.Name != "laptop-renamed" {
			t.Fatalf("second hearing did not update the entry: %+v", beacon)
		}
	}
}

func TestListenerOnFoundReportsEveryHearing(t *testing.T) {
	l := NewListener()
	var calls atomic.Int32
	l.OnFound(func(Beacon) { calls.Add(1) })

	b := Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}
	l.observe(b, beaconTime)
	l.observe(b, beaconTime.Add(2*time.Second))

	if got := calls.Load(); got != 2 {
		t.Fatalf("onFound called %d times, want one per hearing (2)", got)
	}
}

// A host is stopped twice when the Stop button and the window closing both
// fire. The second stop is a no-op; closing the channel twice would panic the
// goroutine that runs it.
func TestAnnouncerStopIsIdempotent(t *testing.T) {
	a := NewAnnouncer("laptop", 8080)

	a.Stop()
	a.Stop()
}
