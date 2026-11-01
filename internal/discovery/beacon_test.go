package discovery

import (
	"encoding/json"
	"fmt"
	"net"
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

// A beacon reaches a host on another segment only by unicast: the broadcast
// dies at the router. The seeds are the addresses this device has dialed before,
// and they get the same beacon, on the beacon port rather than the chat port.
func TestAnnouncerReachesItsSeeds(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: BeaconPort})
	if err != nil {
		t.Skipf("the beacon port is taken on this machine: %v", err)
	}
	defer conn.Close()

	// A chat port on another segment, reached by unicast.
	announcer := NewAnnouncer("laptop", 8080, "127.0.0.1:8080")
	announcer.Start(20 * time.Millisecond)
	defer announcer.Stop()

	if got := announcer.Seeds(); len(got) != 1 || got[0] != "127.0.0.1:8080" {
		t.Fatalf("Seeds() = %v, want the address it was given", got)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	buffer := make([]byte, 512)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("no beacon reached the seed: %v", err)
	}

	var beacon Beacon
	if err := json.Unmarshal(buffer[:n], &beacon); err != nil {
		t.Fatalf("the beacon did not read as one: %v", err)
	}

	if beacon.Name != "laptop" || beacon.Port != 8080 {
		t.Errorf("the beacon says %q on %d, want laptop on 8080", beacon.Name, beacon.Port)
	}
}

// A seed that cannot be reached is skipped, not fatal: the announcer keeps
// going for the other one.
func TestAnnouncerSurvivesAnUnreachableSeed(t *testing.T) {
	announcer := NewAnnouncer("laptop", 8080, "not-a-host", "192.0.2.1:8080")
	announcer.Start(10 * time.Millisecond)
	defer announcer.Stop()

	time.Sleep(50 * time.Millisecond)

	// Nothing to assert but the absence of a panic and a live announcer.
	if len(announcer.Seeds()) != 2 {
		t.Errorf("the announcer forgot its seeds: %v", announcer.Seeds())
	}
}

// A beacon is unauthenticated: whoever is on the segment writes the list. A
// flood of made-up addresses must not grow it without bound — that is a local
// attacker, or one broken host, taking the app down by talking.
func TestListenerBoundsWhatItRemembers(t *testing.T) {
	l := NewListener()

	base := beaconTime
	for i := 0; i < MaxBeacons*3; i++ {
		l.observe(Beacon{Addr: fmt.Sprintf("10.0.%d.%d", i/256, i%256), Port: 8080}, base.Add(time.Duration(i)*time.Millisecond))
	}

	l.mu.RLock()
	held := len(l.found)
	l.mu.RUnlock()

	if held > MaxBeacons {
		t.Errorf("the listener remembers %d devices, want at most %d", held, MaxBeacons)
	}
}

// The device that goes is the quietest one: a flood must not be able to hide a
// device that is announcing itself right now.
func TestListenerEvictsTheQuietest(t *testing.T) {
	l := NewListener()

	// The real device speaks last and often.
	l.observe(Beacon{Addr: "10.0.0.1", Name: "real", Port: 8080}, beaconTime)

	for i := 0; i < MaxBeacons+10; i++ {
		l.observe(Beacon{Addr: fmt.Sprintf("10.9.%d.%d", i/256, i%256), Port: 8080}, beaconTime.Add(time.Duration(i+1)*time.Millisecond))
	}

	// It speaks again, after the flood.
	l.observe(Beacon{Addr: "10.0.0.1", Name: "real", Port: 8080}, beaconTime.Add(time.Hour).Add(-time.Minute))

	// The map, not Found(): Found() filters by the wall clock, and this test
	// moves the clock itself.
	l.mu.RLock()
	_, present := l.found[beaconKey(Beacon{Addr: "10.0.0.1", Name: "real", Port: 8080})]
	l.mu.RUnlock()

	if !present {
		t.Error("the device that was announcing itself was evicted by the flood")
	}
}
