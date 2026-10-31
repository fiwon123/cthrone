//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// These tests drive the gesture #181 added through the real widget tree: a long
// press on a button or a tab label copies that label. On a desktop the gesture
// is the right-click, on a phone the hold; Fyne delivers both as
// SecondaryTappable, so the headless driver can raise the same event.
//
// What is asserted here is the wiring, not the widget: ui/widgets has the
// gesture itself, and these tests check that the app's own buttons and tabs
// carry it, that a normal tap still does what it did, and that a label that
// changes with its state copies the one currently on screen.

func clipboardText(t *testing.T) string {
	t.Helper()

	c := fyne.CurrentApp().Clipboard()
	if c == nil {
		t.Fatal("the test app has no clipboard")
	}

	return c.Content()
}

func TestLongPressCopiesAButtonLabel(t *testing.T) {
	g := newScreenGUI(t, false)

	test.TapSecondary(g.moreBtn)

	if got := clipboardText(t); got != "More options" {
		t.Errorf("clipboard = %q, want the button label", got)
	}

	if g.copyPopup == nil {
		t.Fatal("the copy raised no hint")
	}

	if g.copyPopup.Hidden {
		t.Error("the copy hint is hidden")
	}
}

// A normal tap belongs to the button's action, not to the copy, so the toggle
// still works — and the next long press copies the label the toggle left.
func TestTapTogglesAndTheCopyFollowsTheLabel(t *testing.T) {
	g := newScreenGUI(t, false)

	test.Tap(g.moreBtn)

	if !g.moreOptions {
		t.Fatal("a normal tap did not open the extra options")
	}

	test.TapSecondary(g.moreBtn)

	if got := clipboardText(t); got != "Fewer options" {
		t.Errorf("clipboard = %q, want the label now on screen", got)
	}
}

func TestLongPressCopiesATabLabelWithoutSwitching(t *testing.T) {
	g := newScreenGUI(t, false)

	before := g.tabs.SelectedIndex()
	if before != 0 {
		t.Fatalf("selected tab = %d, want the first one", before)
	}

	setup := g.tabs.ButtonAt(1)
	if setup == nil {
		t.Fatal("the tab bar has no Setup label")
	}

	test.TapSecondary(setup)

	if got := clipboardText(t); got != "Setup" {
		t.Errorf("clipboard = %q, want the tab label", got)
	}

	if got := g.tabs.SelectedIndex(); got != before {
		t.Errorf("the long press selected tab %d, want to stay on %d", got, before)
	}
}

func TestTapSelectsATabAndCopiesNothing(t *testing.T) {
	g := newScreenGUI(t, false)

	devices := g.tabs.ButtonAt(2)
	if devices == nil {
		t.Fatal("the tab bar has no Devices label")
	}

	test.Tap(devices)

	if got := g.tabs.SelectedIndex(); got != 2 {
		t.Errorf("selected tab = %d, want Devices at 2", got)
	}

	if got := clipboardText(t); got != "" {
		t.Errorf("a normal tap copied %q", got)
	}
}

// A phone drops the paperclip button's text to save the width its label needs.
// The label the user long-presses is still the one the button was built with,
// so the clipboard gets "Attach" rather than the empty string it draws.
func TestIconOnlyButtonCopiesItsFullLabel(t *testing.T) {
	g := newScreenGUI(t, true)

	g.attachBtn.Enable()

	test.TapSecondary(g.attachBtn)

	if got := clipboardText(t); got != "Attach" {
		t.Errorf("clipboard = %q, want the full label", got)
	}
}
