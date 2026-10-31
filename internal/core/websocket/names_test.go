package websocketcore

import (
	"reflect"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// fakeBeacons stands in for the discovery listener: a test announces what it
// wants heard and records that Start was called, without touching the network.
type fakeBeacons struct {
	beacons []discovery.Beacon
	started bool
}

func (f *fakeBeacons) Start() { f.started = true }

func (f *fakeBeacons) Found() []discovery.Beacon { return f.beacons }

func withBeacons(t *testing.T, source beaconSource) {
	t.Helper()

	original := newBeaconSource
	t.Cleanup(func() { newBeaconSource = original })

	newBeaconSource = func() beaconSource { return source }
}

func TestBeaconNamesMatchesAddressAndPort(t *testing.T) {
	source := &fakeBeacons{beacons: []discovery.Beacon{
		{Name: "laptop", Addr: "192.168.1.20", Port: 8080},
		{Name: "phone", Addr: "192.168.1.21", Port: 8080},
		// The same address on another port is another device, and the scan
		// asked about this one.
		{Name: "nats", Addr: "192.168.1.20", Port: 9090},
		// A device the scan did not find.
		{Name: "elsewhere", Addr: "192.168.1.99", Port: 8080},
	}}
	withBeacons(t, source)

	got := BeaconNames([]string{"192.168.1.20", "192.168.1.21"}, 8080, time.Millisecond)

	want := map[string]string{"192.168.1.20": "laptop", "192.168.1.21": "phone"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BeaconNames = %v, want %v", got, want)
	}

	if !source.started {
		t.Fatal("the listener was never started, so nothing could be heard")
	}
}

// An announcement with no name names nothing: a row that showed the address as
// a name is the bug this feature fixes.
func TestBeaconNamesIgnoresUnnamedAnnouncements(t *testing.T) {
	source := &fakeBeacons{beacons: []discovery.Beacon{
		{Name: "", Addr: "192.168.1.20", Port: 8080},
	}}
	withBeacons(t, source)

	got := BeaconNames([]string{"192.168.1.20"}, 8080, time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("BeaconNames = %v, want no name for an unnamed announcement", got)
	}
}

// A listener that reported nothing is not an error: the scan keeps its
// addresses and the caller prints them without a name.
func TestBeaconNamesWithoutAHeardBeacon(t *testing.T) {
	withBeacons(t, &fakeBeacons{})

	got := BeaconNames([]string{"192.168.1.20"}, 8080, time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("BeaconNames = %v, want an empty map", got)
	}
}

// A scan that found nothing, or a caller with no window to spend, must not
// start a listener at all.
func TestBeaconNamesSkipsAnEmptyScan(t *testing.T) {
	source := &fakeBeacons{}
	withBeacons(t, source)

	if got := BeaconNames(nil, 8080, time.Second); len(got) != 0 {
		t.Fatalf("BeaconNames with no addresses = %v, want an empty map", got)
	}

	if got := BeaconNames([]string{"192.168.1.20"}, 8080, 0); len(got) != 0 {
		t.Fatalf("BeaconNames with no window = %v, want an empty map", got)
	}

	if source.started {
		t.Fatal("a listener was started for a scan with nothing to name")
	}
}
