//go:build gui

package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	drivermobile "fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// paintedRow finds the row the toolkit is actually showing for a device: the
// object under the user's finger, not one the test built for itself.
//
// Invisible objects are skipped, which is not decoration: the Devices tab is
// hidden while another tab is showing, and a search that ignores visibility
// finds a row no user can click — exactly how the dead row of #280 kept a green
// test.
func paintedRow(root fyne.CanvasObject, want string, depth int) *widgets.Text {
	if root == nil || depth > 12 || !root.Visible() {
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
//
// The click is delivered as the toolkit delivers it, not by calling the row's
// handler: the row's selectable label keeps a child deeper than the wrapper, and
// on a desktop that child used to take the click and do nothing with it (#280).
func TestDeviceRowIsClickableOnADesktop(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{Name: "laptop", Addr: "192.168.1.20", Port: 8080}, seen: time.Now()}}
	g.mu.Unlock()

	g.refreshDeviceList()
	g.tabs.SelectIndex(2) // Devices
	g.win.Content().Refresh()

	row := paintedRow(g.win.Content(), deviceLabel(g.visibleBeacons()[0].Beacon), 0)
	if row == nil {
		t.Fatal("the device row is not in the painted tree while its tab is showing")
	}

	g.hostEntry.SetText("")
	g.portEntry.SetText("")

	tapFromTheToolkit(t, g.win.Content(), row)

	if got := g.hostEntry.Text; got != "192.168.1.20" {
		t.Errorf("clicking a device row filled %q, want the row's address", got)
	}

	if got := g.portEntry.Text; got != "8080" {
		t.Errorf("clicking a device row filled port %q, want the row's port", got)
	}
}

// The row a user sees is the row that reacts — on both layouts.
//
// #236 was the regression this guards: a row that is tappable in the test's
// hand but not in the tree the toolkit paints. It asks the painted object, not
// a fresh one, and it taps twice, because a row that works once and then ignores
// every later tap is exactly what "I cannot click it" means.
//
// The tab is switched to Devices first, and back again before the second tap:
// the row is only on screen while its own tab is, and the first tap moves to
// Setup. A test that skips that is tapping a hidden row (#280).
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

			want := deviceLabel(g.visibleBeacons()[0].Beacon)
			if want != "192.168.15.25:8080" {
				t.Fatalf("the row reads %q, want the address and port the user reported", want)
			}

			for tap := 1; tap <= 2; tap++ {
				g.tabs.SelectIndex(2) // Devices
				g.win.Content().Refresh()

				row := paintedRow(g.win.Content(), want, 0)
				if row == nil {
					t.Fatalf("tap %d: the address row is not in the painted tree at all", tap)
				}

				g.hostEntry.SetText("")
				g.portEntry.SetText("")

				tapFromTheToolkit(t, g.win.Content(), row)

				if got := g.hostEntry.Text; got != "192.168.15.25" {
					t.Fatalf("tap %d filled %q, want the address", tap, got)
				}

				if got := g.portEntry.Text; got != "8080" {
					t.Fatalf("tap %d filled port %q, want 8080", tap, got)
				}

				// And the tap goes to Setup, where the address it filled is.
				if got := g.tabs.SelectedIndex(); got != 1 {
					t.Errorf("tap %d selected tab %d, want Setup", tap, got)
				}
			}
		})
	}
}

// The layer that carries the tap must not carry the drag. On a desktop a drag
// still has to reach the label's selection child — that is how a mouse selects
// the address — and on a phone it still has to reach the pane, or the list
// cannot be scrolled by touch.
func TestTheTapLayerLeavesTheDragAlone(t *testing.T) {
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
			g.tabs.SelectIndex(2) // Devices
			g.win.Content().Refresh()

			row := paintedRow(g.win.Content(), deviceLabel(g.visibleBeacons()[0].Beacon), 0)
			if row == nil {
				t.Fatal("the address row is not in the painted tree")
			}

			start, ok := canvasPosition(g.win.Content(), row)
			if !ok {
				t.Fatal("the address row has no position")
			}

			centre := start.Add(fyne.NewPos(row.Size().Width/2, row.Size().Height/2))

			tapTarget, _ := hitTest(g.win.Content(), centre)

			// Each driver picks the drag target from the interfaces it knows:
			// the desktop one from Draggable, the mobile one from Draggable and
			// then Touchable.
			dragged, _ := hitTestMatching(g.win.Content(), centre, func(obj fyne.CanvasObject) bool {
				if _, draggable := obj.(fyne.Draggable); draggable {
					return true
				}

				if !mobile {
					return false
				}

				_, touchable := obj.(drivermobile.Touchable)

				return touchable
			})

			if dragged == nil {
				t.Fatal("nothing under the row takes a drag, so its text can neither be selected nor scrolled")
			}

			// One object must not take both: a layer that took the drag as well
			// as the tap would stop the selection or the scroll.
			if dragged == tapTarget {
				t.Fatalf("%T takes both the tap and the drag", dragged)
			}

			if mobile {
				// The pane above the row is what scrolls; a target inside the
				// row would take the scroll away from the list.
				if !treeContains(dragged, row) {
					t.Fatalf("a phone drag lands on %T inside the row, so the list would not scroll", dragged)
				}

				return
			}

			// A desktop drag belongs to the label's own selection child, which
			// lives inside the row.
			if dragged == fyne.CanvasObject(row) || !treeContains(row, dragged) {
				t.Fatalf("a desktop drag lands on %T, not on the row's selection", dragged)
			}
		})
	}
}

// A long press on a phone still copies the address, through the layer: the press
// lands on the layer, which hands it to the text, which brings Fyne's selection
// up and offers Copy.
func TestDeviceRowStillCopiesOnAPhoneThroughTheTapLayer(t *testing.T) {
	g := newScreenGUI(t, true)

	g.mu.Lock()
	g.beacons = []device{{Beacon: discovery.Beacon{Addr: "192.168.15.25", Port: 8080}, seen: time.Now()}}
	g.mu.Unlock()

	g.refreshDeviceList()
	g.win.Resize(fyne.NewSize(420, 640))
	g.tabs.SelectIndex(2) // Devices
	g.win.Content().Refresh()

	row := paintedRow(g.win.Content(), deviceLabel(g.visibleBeacons()[0].Beacon), 0)
	if row == nil {
		t.Fatal("the address row is not in the painted tree")
	}

	start, ok := canvasPosition(g.win.Content(), row)
	if !ok {
		t.Fatal("the address row has no position")
	}

	centre := start.Add(fyne.NewPos(row.Size().Width/2, row.Size().Height/2))

	found, _ := hitTest(g.win.Content(), centre)
	press, ok := found.(fyne.SecondaryTappable)
	if !ok {
		t.Fatalf("a long press lands on %T, which does not take one", found)
	}

	press.TappedSecondary(&fyne.PointEvent{Position: centre, AbsolutePosition: centre})

	if !row.Selectable() {
		t.Error("the long press selected nothing, so the address cannot be copied")
	}

	if g.win.Canvas().Overlays().Top() == nil {
		t.Error("the long press offered no Copy menu")
	}
}
