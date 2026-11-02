//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2/widget"
)

// The composer's actions read like a chat app's: a paperclip, and Send as the
// primary action (#324). This is the shape, not the pixels.
func TestComposerActionsReadLikeAChatApp(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			if got := g.attachBtn.Text(); got != "" {
				t.Errorf("the attach action shows %q, want the paperclip alone", got)
			}

			if got := g.sendBtn.Importance(); got != widget.HighImportance {
				t.Errorf("Send has importance %v, want the primary one", got)
			}
		})
	}
}
