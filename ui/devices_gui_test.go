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

// paintedRow finds the row the toolkit is actually showing for a device: the
// object under the user's finger, not one the test built for itself.
func paintedRow(root fyne.CanvasObject, want string, depth int) *widgets.Text {
	if root == nil || depth > 12 {
		return nil
	}

	if text, ok := root.(*widgets.Text); ok && text.Text() == want {
		return text
	}

	if box, ok := root.(*fyne.Container); ok {
		for _, child := range box.Objects {
			if found := paintedRow(child, want, depth+1); found != nil {
				return found
			}
		}
	}

	if drawn, ok := root.(fyne.Widget); ok {
		for _, child := range test.WidgetRenderer(drawn).Objects() {
			if found := paintedRow(child, want, depth+1); found != nil {
				return found
			}
		}
	}

	return nil
}

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

// The row a user sees is the row that reacts — on both layouts.
//
// #236 was the regression this guards: a row that is tappable in the test's
// hand but not in the tree the toolkit paints. It asks the painted object, not
// a fresh one, and it taps twice, because a row that works once and then ignores
// every later tap is exactly what "I cannot click it" means.
func TestBeaconRowTheUserSeesIsTappable(t *testing.T) {
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
			g.win.Content().Refresh()

			want := deviceLabel(g.visibleBeacons()[0].Beacon)
			if want != "192.168.15.25:8080" {
				t.Fatalf("the row reads %q, want the address and port the user reported", want)
			}

			row := paintedRow(g.win.Content(), want, 0)
			if row == nil {
				t.Fatal("the address row is not in the painted tree at all")
			}

			tappable, ok := any(row).(fyne.Tappable)
			if !ok {
				t.Fatalf("the painted row is %T, which cannot be tapped", row)
			}

			for tap := 1; tap <= 2; tap++ {
				g.hostEntry.SetText("")
				g.portEntry.SetText("")

				test.Tap(tappable)

				if got := g.hostEntry.Text; got != "192.168.15.25" {
					t.Fatalf("tap %d filled %q, want the address", tap, got)
				}

				if got := g.portEntry.Text; got != "8080" {
					t.Fatalf("tap %d filled port %q, want 8080", tap, got)
				}
			}

			// And the tap goes to Setup, where the address it filled is.
			if got := g.tabs.SelectedIndex(); got != 1 {
				t.Errorf("tapping a device selected tab %d, want Setup", got)
			}
		})
	}
}
