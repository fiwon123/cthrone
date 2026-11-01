//go:build linux && !android

package bluetoothcore

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
	bluetoothhandler "github.com/fiwon123/cthrone/internal/handlers/bluetooth"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// fakeLink is one end of an in-memory BLE link, as far as the session above it
// can tell. No radio is opened: the transport's loop is what these tests check.
type fakeLink struct {
	name string

	incoming chan []byte

	mu     sync.Mutex
	reason string
	closed bool
}

func newFakeLink(name string) *fakeLink {
	return &fakeLink{name: name, incoming: make(chan []byte, 16)}
}

func (l *fakeLink) Name() string            { return l.name }
func (l *fakeLink) MTU() int                { return 20 }
func (l *fakeLink) Write([]byte) error      { return nil }
func (l *fakeLink) Incoming() <-chan []byte { return l.incoming }
func (l *fakeLink) Reason() string          { l.mu.Lock(); defer l.mu.Unlock(); return l.reason }
func (l *fakeLink) Close(reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.closed = true
	l.reason = reason
	close(l.incoming)

	return nil
}

// fakeListener hands out the links it was given, and reports that it stopped
// once Close is called.
type fakeListener struct {
	links chan bluetoothhandler.Link
	done  chan struct{}
	once  sync.Once
}

func newFakeListener(links ...bluetoothhandler.Link) *fakeListener {
	l := &fakeListener{links: make(chan bluetoothhandler.Link, len(links)), done: make(chan struct{})}

	for _, link := range links {
		l.links <- link
	}

	return l
}

func (l *fakeListener) Accept() (bluetoothhandler.Link, error) {
	select {
	case link := <-l.links:
		return link, nil
	case <-l.done:
		return nil, bluetoothhandler.ErrNotice
	}
}

func (l *fakeListener) Close() error {
	l.once.Do(func() { close(l.done) })

	return nil
}

// noPins is the pairing options a test uses: no token and no file, so the
// device key is the credential.
func noPins() pairing.Options { return pairing.Options{} }

// waitFor waits until cond holds, or fails the test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for i := 0; i < 200; i++ {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// A host starts, serves and stops: the listener is what Stop closes, and Wait
// returns once it has.
func TestOpenServerServesAndStops(t *testing.T) {
	fake := newFakeListener()

	original := listen
	t.Cleanup(func() { listen = original })

	listen = func(string) (listener, error) { return fake, nil }

	h, err := OpenServer(app.New(8080), noPins(), func(session.Session) {})
	if err != nil {
		t.Fatalf("OpenServer() error = %v", err)
	}

	if h.Hub() == nil {
		t.Fatal("the host has no hub")
	}

	if h.DeviceFingerprint == "" {
		t.Error("the host reports no device fingerprint to compare")
	}

	stopped := make(chan struct{})

	go func() {
		_ = h.Wait()
		close(stopped)
	}()

	if err := h.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after Stop()")
	}
}

// Stop is idempotent: the console and the caller may both end the host.
func TestStopTwiceIsSafe(t *testing.T) {
	fake := newFakeListener()

	original := listen
	t.Cleanup(func() { listen = original })

	listen = func(string) (listener, error) { return fake, nil }

	h, err := OpenServer(app.New(8080), noPins(), func(session.Session) {})
	if err != nil {
		t.Fatalf("OpenServer() error = %v", err)
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

// A device that connects but never proves a key reaches neither the room nor
// the consumer. The radio is not a door: the handshake is.
func TestAnUnauthenticatedDeviceIsNotAdmitted(t *testing.T) {
	link := newFakeLink("stranger")
	fake := newFakeListener(link)

	original := listen
	t.Cleanup(func() { listen = original })

	listen = func(string) (listener, error) { return fake, nil }

	h, err := OpenServer(app.New(8080), noPins(), func(session.Session) {
		t.Error("a device that never authenticated reached the consumer")
	})
	if err != nil {
		t.Fatalf("OpenServer() error = %v", err)
	}
	t.Cleanup(func() { _ = h.Stop() })

	// Give the accept loop time to seal the link and start a handshake that
	// nobody answers.
	time.Sleep(100 * time.Millisecond)

	if count := h.Hub().Count(); count != 0 {
		t.Fatalf("the room holds %d device(s) that never authenticated", count)
	}
}

// Connect refuses a dial that found nothing, and the error is the one the
// transport gave rather than a generic one.
func TestConnectReportsAFailedDial(t *testing.T) {
	want := errors.New("no device")

	original := dial
	t.Cleanup(func() { dial = original })

	dial = func(string, time.Duration) (bluetoothhandler.Link, error) { return nil, want }

	if err := Connect(nil, app.New(8080), noPins()); !errors.Is(err, want) {
		t.Fatalf("Connect() error = %v, want %v", err, want)
	}
}

// OpenSession seals what it dials: the caller gets an e2e session, not the raw
// link, and closing it closes the link.
func TestOpenSessionSealsTheLink(t *testing.T) {
	link := newFakeLink("alice")

	original := dial
	t.Cleanup(func() { dial = original })

	dial = func(string, time.Duration) (bluetoothhandler.Link, error) { return link, nil }

	s, err := OpenSession([]string{"AA:BB:CC:DD:EE:FF"}, app.New(8080), noPins())
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}

	if _, raw := s.(interface{ Link() bluetoothhandler.Link }); raw {
		t.Fatal("OpenSession returned the raw link")
	}

	s.Close("bye")

	waitFor(t, "the link to close", func() bool {
		link.mu.Lock()
		defer link.mu.Unlock()

		return link.closed
	})
}

// The argument names the device, and no argument takes the first one in range.
func TestTargetFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"none", nil, ""},
		{"a name", []string{"phone"}, "phone"},
		{"an address", []string{"AA:BB:CC:DD:EE:FF"}, "AA:BB:CC:DD:EE:FF"},
		{"the first of several", []string{"phone", "laptop"}, "phone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := target(tt.args); got != tt.want {
				t.Fatalf("target(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// Scan reports what it found, one device per line, with the name in front of
// the address so two devices of the same name can still be told apart.
func TestScanReportsDevices(t *testing.T) {
	original := listScan
	t.Cleanup(func() { listScan = original })

	listScan = func(_ time.Duration, report func(bluetoothhandler.Peer)) error {
		report(bluetoothhandler.Peer{Address: "AA:BB:CC:DD:EE:01", Name: "phone", RSSI: -40})
		report(bluetoothhandler.Peer{Address: "AA:BB:CC:DD:EE:02", Name: "laptop", RSSI: -70})

		return nil
	}

	var out bytes.Buffer

	if err := Scan(&out); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	printed := out.String()

	for _, want := range []string{"phone", "laptop", "AA:BB:CC:DD:EE:01", "Found 2"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the scan did not print %q:\n%s", want, printed)
		}
	}
}

// Nothing in range says so, and says why the next attempt might work: the
// device has to be hosting, and within range.
func TestScanReportsNothingInRange(t *testing.T) {
	original := listScan
	t.Cleanup(func() { listScan = original })

	listScan = func(time.Duration, func(bluetoothhandler.Peer)) error { return nil }

	var out bytes.Buffer

	if err := Scan(&out); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if !strings.Contains(out.String(), "No cthrone device answered over Bluetooth") {
		t.Fatalf("the scan printed:\n%s", out.String())
	}
}

// ScanPeers hands the found devices to its caller instead of printing them: the
// GUI fills its Devices list from the same walk the terminal prints.
func TestScanPeersReportsEveryPeer(t *testing.T) {
	original := listScan
	t.Cleanup(func() { listScan = original })

	var asked time.Duration

	listScan = func(timeout time.Duration, report func(bluetoothhandler.Peer)) error {
		asked = timeout
		report(bluetoothhandler.Peer{Address: "AA:BB:CC:DD:EE:01", Name: "phone", RSSI: -40})

		return nil
	}

	var got []bluetoothhandler.Peer

	if err := ScanPeers(3*time.Second, func(p bluetoothhandler.Peer) { got = append(got, p) }); err != nil {
		t.Fatalf("ScanPeers() error = %v", err)
	}

	if asked != 3*time.Second {
		t.Errorf("the scan ran for %s, want the timeout the caller named", asked)
	}

	if len(got) != 1 || got[0].Address != "AA:BB:CC:DD:EE:01" || got[0].Name != "phone" {
		t.Fatalf("ScanPeers() reported %+v, want the peer the scan found", got)
	}
}

// A scan that cannot use the adapter reports the reason instead of an empty
// list, which would read as "nothing is there".
func TestScanReportsAnUnusableAdapter(t *testing.T) {
	original := listScan
	t.Cleanup(func() { listScan = original })

	listScan = func(time.Duration, func(bluetoothhandler.Peer)) error {
		return bluetoothhandler.ErrUnavailable
	}

	var out bytes.Buffer

	if err := Scan(&out); !errors.Is(err, bluetoothhandler.ErrUnavailable) {
		t.Fatalf("Scan() error = %v, want ErrUnavailable", err)
	}
}
