//go:build gui

package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// A device row is text a user copies — the address of the machine they are
// about to dial — so it is the same long-press widget the transcript uses.
// Its tap, though, still belongs to the list: tapping a row fills the address
// field, which is the whole reason the row is there.
func TestDeviceRowCopiesOnAPhoneAndStillFillsOnATap(t *testing.T) {
	g := newScreenGUI(t, true)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{
		Name: "laptop",
		Addr: "192.168.1.20",
		Port: 8080,
	}, seen: time.Now()}}
	g.mu.Unlock()

	row := g.beaconList.CreateItem()

	text, ok := row.(*widgets.Text)
	if !ok {
		t.Fatalf("a device row is %T, want the long-press text", row)
	}

	// The real binding, not a copy of it: the text and the tap both come from
	// updateBeaconRow, which is what the list calls.
	g.updateBeaconRow(0, text)

	if text.Selectable() {
		t.Error("a phone's device row is selectable before any long press")
	}

	// The long press is what selects: the label becomes selectable and Fyne's own
	// selection comes on.
	text.TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(10, 5)})

	if !text.Selectable() {
		t.Error("a long press left the device row unselectable, so the address cannot be copied")
	}

	// The tap is still the list's: it fills the address field.
	g.hostEntry.SetText("")
	test.Tap(text)

	if got := g.hostEntry.Text; got != "192.168.1.20" {
		t.Errorf("tapping a device row filled %q, want the row's address", got)
	}
}

// A desktop row selects by dragging, as every other label there does.
func TestDesktopDeviceRowSelectsFromTheStart(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}, seen: time.Now()}}
	g.mu.Unlock()

	row := g.beaconList.CreateItem()

	text, ok := row.(*widgets.Text)
	if !ok {
		t.Fatalf("a device row is %T, want the long-press text", row)
	}

	if !text.Selectable() {
		t.Error("a desktop's device row is not selectable, so its address cannot be dragged over")
	}
}

// Clicking a device row is how a desktop fills the address field, and wrapping
// the row in a widget that takes taps must not take the click with it: the
// wrapper is what the driver finds, so it has to run the list's action (#236).
func TestDeviceRowIsClickableOnADesktop(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}, seen: time.Now()}}
	g.mu.Unlock()

	row := g.beaconList.CreateItem()
	g.updateBeaconRow(0, row)

	tappable, ok := row.(fyne.Tappable)
	if !ok {
		t.Fatalf("a device row is %T, which cannot be tapped at all", row)
	}

	g.hostEntry.SetText("")
	g.portEntry.SetText("")

	test.Tap(tappable)

	if got := g.hostEntry.Text; got != "192.168.1.20" {
		t.Errorf("tapping a device row filled %q, want the row's address", got)
	}

	if got := g.portEntry.Text; got != "8080" {
		t.Errorf("tapping a device row filled port %q, want the row's port", got)
	}
}
