//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// pressClear taps the clear action inside a field.
func pressClear(t *testing.T, entry *widget.Entry) {
	t.Helper()

	if entry.ActionItem == nil {
		t.Fatal("the field has no clear action")
	}

	tappable, ok := entry.ActionItem.(fyne.Tappable)
	if !ok {
		t.Fatalf("the clear action is %T, which cannot be pressed", entry.ActionItem)
	}

	test.Tap(tappable)
}

// Holding backspace does not repeat on a phone — the toolkit's Android keyboard
// bridge rewrites its one-character proxy after every delete, tracked upstream —
// so a long message needs one press that empties the field (#290). It is offered
// on both layouts: the defect is the phone's, the action costs nothing on a
// desktop.
func TestTheComposerCanBeEmptiedWithOnePress(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			g.entry.SetText("a long message that would take one press per letter")

			pressClear(t, g.entry)

			if got := g.entry.Text; got != "" {
				t.Errorf("the clear action left %q in the composer", got)
			}
		})
	}
}

// The fields a user types into carry the same action, which is what "any input
// field" asked for (#290).
func TestTheSetupFieldsCanBeEmptiedWithOnePress(t *testing.T) {
	g := newScreenGUI(t, false)

	tests := []struct {
		name  string
		entry *widget.Entry
	}{
		{"device name", g.nameEntry},
		{"connect to", g.hostEntry},
		{"fingerprint", g.fingerprintEntry},
		{"NATS subject", g.subjectEntry},
		{"bind address", g.bindEntry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.SetText("a value someone typed")

			pressClear(t, tt.entry)

			if got := tt.entry.Text; got != "" {
				t.Errorf("the clear action left %q in the %s field", got, tt.name)
			}
		})
	}
}
