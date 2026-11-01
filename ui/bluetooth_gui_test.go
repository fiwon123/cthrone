//go:build gui

package ui

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// These tests drive the Bluetooth transport from the window: which option the
// select offers, what the Setup tab keeps when it is chosen, which rows the
// Devices list shows, and what Connect and Host pass down to the transport.
//
// No radio is opened. The transport's own entry points are variables, so the
// window can be checked without an adapter — the same reason cmd/ does the same
// thing. Nothing here proves a radio link; that needs two devices.

// chooseTransport puts the Setup tab on a transport, the way a user does.
func chooseTransport(t *testing.T, g *gui, mode transportMode) {
	t.Helper()

	g.transport.SetSelected(transportName(mode))
}

// formCaptions is the caption of every row the Setup form is showing. The form
// is rebuilt into a stack, so a row that is not in the subset leaves nothing
// behind — which is exactly what has to be checked.
func formCaptions(t *testing.T, g *gui) []string {
	t.Helper()

	if len(g.formHolder.Objects) != 1 {
		t.Fatalf("the form holder has %d objects, want 1", len(g.formHolder.Objects))
	}

	var captions []string

	collectCaptions(g.formHolder.Objects[0], &captions)

	return captions
}

// collectCaptions walks a form and keeps the labels in it. widget.Form's items
// are drawn from caption labels and field widgets, so the labels are the
// captions — a field that is itself a Select or an Entry is not one.
func collectCaptions(obj fyne.CanvasObject, into *[]string) {
	if label, ok := obj.(*widget.Label); ok {
		*into = append(*into, label.Text)

		return
	}

	if box, ok := obj.(*fyne.Container); ok {
		for _, child := range box.Objects {
			collectCaptions(child, into)
		}
	}
}

// The select offers what the build has, and the layout no longer decides it: a
// phone-shaped window on this build still offers the transport this binary was
// compiled with, and a build without it offers only the network transports on
// every layout.
func TestTransportSelectFollowsTheBuild(t *testing.T) {
	g := newScreenGUI(t, false)

	// Whatever this build answers, the window has to agree with it: the list is
	// not a literal in the GUI, it is the capability.
	if got, want := g.transport.Options, transportNames(bluetoothcore.Supported()); !slices.Equal(got, want) {
		t.Fatalf("the select offers %v, want %v", got, want)
	}

	// Flip the capability the way a Windows or macOS build answers it, then
	// build the window that platform would have.
	restore := bluetoothSupported
	bluetoothSupported = func() bool { return false }
	t.Cleanup(func() { bluetoothSupported = restore })

	for _, mobile := range []bool{false, true} {
		g := newScreenGUI(t, mobile)

		for _, name := range g.transport.Options {
			if name == labelBluetooth {
				t.Errorf("a build without the radio offers %q (mobile=%v): %v", name, mobile, g.transport.Options)
			}
		}

		if got := g.transport.Options; !slices.Equal(got, transportNames(false)) {
			t.Errorf("the select offers %v, want %v", got, transportNames(false))
		}
	}
}

// Choosing Bluetooth takes away every row that configures a socket or a
// certificate, and keeps the ones the radio can actually use.
func TestBluetoothModeShowsOnlyTheRowsItUses(t *testing.T) {
	g := newScreenGUI(t, false)

	chooseTransport(t, g, transportBluetooth)

	captions := strings.Join(formCaptions(t, g), " | ")

	for _, gone := range []string{"Port", "Bind address"} {
		if strings.Contains(captions, gone) {
			t.Errorf("the Bluetooth form still shows the %q row: %s", gone, captions)
		}
	}

	for _, want := range []string{"Device name", "Connect to", "Transport"} {
		if !strings.Contains(captions, want) {
			t.Errorf("the Bluetooth form is missing the %q row: %s", want, captions)
		}
	}

	// The hint and the Scan button are the two places a user reads what the
	// carrier is before pressing anything.
	if hint := g.hint.Text(); !strings.Contains(hint, "Bluetooth") {
		t.Errorf("the hint does not name the transport: %q", hint)
	}

	if hint := g.listHint.Text(); !strings.Contains(hint, "Bluetooth") {
		t.Errorf("the scan hint does not name the transport: %q", hint)
	}
}

// Choosing the websocket again brings its rows back: the mode is a view over
// the same widgets, not a rebuild that loses what was typed.
func TestSwitchingBackRestoresTheNetworkRows(t *testing.T) {
	g := newScreenGUI(t, false)

	g.portEntry.SetText("9191")
	chooseTransport(t, g, transportBluetooth)
	chooseTransport(t, g, transportBluetooth)
	chooseTransport(t, g, transportWebsocket)

	captions := strings.Join(formCaptions(t, g), " | ")
	if !strings.Contains(captions, "Port") {
		t.Errorf("the websocket form lost its port row: %s", captions)
	}

	if got := g.portEntry.Text; got != "9191" {
		t.Errorf("the port field reads %q, want what the user typed", got)
	}
}

// A radio scan's rows are the only ones the Devices list offers while the radio
// is selected: a Bluetooth address is not something the websocket can dial.
func TestBluetoothModeListsOnlyRadioDevices(t *testing.T) {
	g := newTestGUI(t)

	g.addBeacon(beaconFor("192.168.1.20", 8080, "laptop"))
	g.addPeer(bluetoothcore.Peer{Address: "AA:BB:CC:DD:EE:01", Name: "phone"})

	rows := g.visibleBeacons()
	if len(rows) != 1 || rows[0].Addr != "192.168.1.20" {
		t.Fatalf("the network mode lists %+v, want only the network device", rows)
	}

	chooseTransport(t, g, transportBluetooth)

	rows = g.visibleBeacons()
	if len(rows) != 1 {
		t.Fatalf("the radio mode lists %d rows, want 1", len(rows))
	}

	if !rows[0].Radio || rows[0].Addr != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("the radio mode lists %+v, want the Bluetooth device", rows[0])
	}

	// Selecting a radio row fills the dial field with what the transport takes.
	g.selectBeacon(0)

	if got := g.hostEntry.Text; got != "AA:BB:CC:DD:EE:01" {
		t.Errorf("Connect to reads %q, want the Bluetooth address", got)
	}

	// The list shows the label without a port, because a radio address has none.
	if label, want := deviceLabel(rows[0].Beacon), "phone — AA:BB:CC:DD:EE:01"; label != want {
		t.Errorf("the row reads %q, want %q", label, want)
	}
}

// Connect with nothing typed means "the first device in range" on the radio, so
// it must not be refused the way an empty websocket address is.
func TestConnectBluetoothWithNoTargetTakesTheFirstDevice(t *testing.T) {
	g := newTestGUI(t)

	chooseTransport(t, g, transportBluetooth)

	recorder := newSendRecorder()

	var (
		mu     sync.Mutex
		target []string
	)

	original := openBluetoothSession
	t.Cleanup(func() { openBluetoothSession = original })

	openBluetoothSession = func(args []string, data *app.Data, opts pairing.Options) (session.Session, error) {
		mu.Lock()
		target = args
		mu.Unlock()

		return recorder, nil
	}

	g.connect()

	waitForSession(t, g, recorder)

	mu.Lock()
	defer mu.Unlock()

	if len(target) != 0 {
		t.Fatalf("Connect passed %v to the transport, want no target at all", target)
	}

	if got := g.status.Text(); !strings.Contains(got, "Bluetooth") {
		t.Errorf("the status reads %q, want it to say the session is on the radio", got)
	}
}

// A named device is what the transport is asked for, so two phones in range can
// still be told apart.
func TestConnectBluetoothNamesTheChosenDevice(t *testing.T) {
	g := newTestGUI(t)

	chooseTransport(t, g, transportBluetooth)
	g.hostEntry.SetText("AA:BB:CC:DD:EE:02")

	recorder := newSendRecorder()

	var (
		mu     sync.Mutex
		target []string
	)

	original := openBluetoothSession
	t.Cleanup(func() { openBluetoothSession = original })

	openBluetoothSession = func(args []string, data *app.Data, opts pairing.Options) (session.Session, error) {
		mu.Lock()
		target = args
		mu.Unlock()

		return recorder, nil
	}

	g.connect()
	waitForSession(t, g, recorder)

	mu.Lock()
	defer mu.Unlock()

	if len(target) != 1 || target[0] != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("Connect passed %v, want the address that was typed", target)
	}
}

// A radio that cannot be opened reports what went wrong on the screen: a
// failure that only reaches a log is a failure nobody sees.
func TestConnectBluetoothReportsTheTransportsFailure(t *testing.T) {
	g := newTestGUI(t)

	chooseTransport(t, g, transportBluetooth)

	original := openBluetoothSession
	t.Cleanup(func() { openBluetoothSession = original })

	failure := errors.New("bluetooth: no usable bluetooth adapter")
	openBluetoothSession = func([]string, *app.Data, pairing.Options) (session.Session, error) {
		return nil, failure
	}

	g.connect()

	waitForStatus(t, g, func() bool { return g.status.Text() == "Connection failed" })

	if g.sess != nil {
		t.Error("a failed connect attached a session anyway")
	}
}

// Hosting over the radio shows this device's fingerprint and no copyable
// secret, and the handle is the thing Stop reaches.
func TestHostBluetoothOffersTheFingerprintAndStops(t *testing.T) {
	g := newTestGUI(t)

	chooseTransport(t, g, transportBluetooth)

	handle := &bluetoothcore.HostHandle{DeviceFingerprint: "fp-from-the-radio"}

	original := openBluetoothServer
	t.Cleanup(func() { openBluetoothServer = original })

	var openerCalled sync.WaitGroup
	openerCalled.Add(1)

	openBluetoothServer = func(data *app.Data, opts pairing.Options, onSession func(session.Session), confirm ...bluetoothcore.Confirm) (*bluetoothcore.HostHandle, error) {
		openerCalled.Done()

		if len(confirm) != 1 || confirm[0] == nil {
			t.Error("the host was started with no first-contact gate")
		}

		return handle, nil
	}

	g.host()

	waitForStatus(t, g, func() bool { return strings.Contains(g.status.Text(), "Bluetooth") })

	openerCalled.Wait()

	g.mu.Lock()
	server := g.server
	g.mu.Unlock()

	if server != handle {
		t.Fatalf("the window holds %T as its server, want the Bluetooth host", server)
	}

	// A radio host has no address to advertise on screen: the other device
	// finds it by scanning, and the dialog carries the fingerprint instead.
	if g.thisDevice.Visible() {
		t.Error("the radio host shows an address line, which it has no address for")
	}

	g.stop()

	waitForStatus(t, g, func() bool { return g.status.Text() == "Disconnected" })

	g.mu.Lock()
	still := g.server
	g.mu.Unlock()

	if still != nil {
		t.Errorf("Stop left the Bluetooth host installed: %T", still)
	}

	// The handle is stopped by the window, not just forgotten.
	if err := handle.Stop(); err != nil {
		t.Errorf("the handle was not stoppable after the window dropped it: %v", err)
	}
}

// waitForSession waits until the window has attached the session a stub
// returned. The dial runs on a goroutine, so a test cannot read the state
// straight after the call.
func waitForSession(t *testing.T, g *gui, want session.Session) {
	t.Helper()

	for i := 0; i < 400; i++ {
		g.mu.Lock()
		got := g.sess
		g.mu.Unlock()

		if got == want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the window never attached the session the transport returned")
}

// waitForStatus waits until the status line satisfies cond. setStatus posts to
// the UI goroutine.
func waitForStatus(t *testing.T, g *gui, cond func() bool) {
	t.Helper()

	for i := 0; i < 400; i++ {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("the status line never reached the expected value: %q", g.status.Text())
}

// beaconFor is one network device as a beacon would announce it.
func beaconFor(addr string, port int, name string) discovery.Beacon {
	return discovery.Beacon{Addr: addr, Port: port, Name: name}
}
