//go:build gui

package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// A device row is a button, because that is what it does (#287).
//
// It used to be the transcript's long-press text, which made the address
// selectable and made the row look like a sentence. A user reported exactly
// that: "it seems is just a select text not a button". The address is still
// readable and copyable where it is used — the Setup field it fills, and the
// status line that names the selection.
func TestADeviceRowIsAButton(t *testing.T) {
	g := newScreenGUI(t, false)

	row := g.beaconList.CreateItem()

	button, ok := row.(*widget.Button)
	if !ok {
		t.Fatalf("a device row is %T, want a button the user can press", row)
	}

	if button.Alignment != widget.ButtonAlignLeading {
		t.Error("the device address is centred, so a list of them does not line up")
	}
}

// Pressing the row a user can see fills the address and shows Setup — on both
// layouts, and twice, because a row that answers once and then ignores later
// presses is what "I cannot press it" means.
//
// The press is delivered the way the toolkit delivers it, and the Devices tab is
// selected first: a row in a hidden tab is not a row a user can press (#280).
func TestTheDeviceRowTheUserSeesFillsTheAddress(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			g.mu.Lock()
			g.beacons = []device{{Beacon: discovery.Beacon{Addr: "192.168.15.25", Port: 8080}, seen: time.Now()}}
			g.mu.Unlock()

			g.refreshDeviceList()
			g.win.Resize(fyne.NewSize(420, 640))

			want := deviceLabel(g.visibleBeacons()[0].Beacon)
			if want != "192.168.15.25:8080" {
				t.Fatalf("the row reads %q, want the address and port the user reported", want)
			}

			for press := 1; press <= 2; press++ {
				g.tabs.SelectIndex(2) // Devices
				g.win.Content().Refresh()

				row := paintedButton(g.win.Content(), want, 0)
				if row == nil {
					t.Fatalf("press %d: the address row is not in the painted tree at all", press)
				}

				g.hostEntry.SetText("")
				g.portEntry.SetText("")

				tapFromTheToolkit(t, g.win.Content(), row)

				if got := g.hostEntry.Text; got != "192.168.15.25" {
					t.Fatalf("press %d filled %q, want the address", press, got)
				}

				if got := g.portEntry.Text; got != "8080" {
					t.Fatalf("press %d filled port %q, want 8080", press, got)
				}

				// And the press goes to Setup, where the address it filled is.
				if got := g.tabs.SelectedIndex(); got != 1 {
					t.Errorf("press %d selected tab %d, want Setup", press, got)
				}
			}
		})
	}
}

// A radio row names a device rather than an address on a network, so pressing it
// fills the name and leaves the port alone — the field is not even on screen in
// that mode.
func TestABluetoothRowFillsTheDeviceAndNoPort(t *testing.T) {
	g := newScreenGUI(t, true)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{Addr: "AA:BB:CC:DD:EE:FF", Name: "phone"}, Radio: true, seen: time.Now()}}
	g.mu.Unlock()

	chooseTransport(t, g, transportBluetooth)
	g.refreshDeviceList()
	g.win.Resize(fyne.NewSize(420, 640))
	g.tabs.SelectIndex(2) // Devices
	g.win.Content().Refresh()

	want := deviceLabel(g.visibleBeacons()[0].Beacon)

	row := paintedButton(g.win.Content(), want, 0)
	if row == nil {
		t.Fatalf("the radio row %q is not in the painted tree", want)
	}

	g.hostEntry.SetText("")
	g.portEntry.SetText("9999")

	tapFromTheToolkit(t, g.win.Content(), row)

	if got := g.hostEntry.Text; got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("pressing a radio row filled %q, want the address", got)
	}

	if got := g.portEntry.Text; got != "9999" {
		t.Errorf("pressing a radio row changed the port to %q; a radio address has none", got)
	}
}
