//go:build gui

package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"github.com/fiwon123/cthrone/internal/e2e"
)

// confirmFirstContact asks the user whether a device met for the first time may
// join, and is the answer the end-to-end handshake waits on.
//
// It runs on the handshake goroutine, so it hands the dialog to the UI thread
// and blocks here until the question is answered. Nothing is pinned before the
// answer: the peer's session is not attached, and this side has not sent the
// confirm that would authenticate it.
//
// The question expires on the same window the handshake gives it, so an
// abandoned dialog asks nothing of a session that has already been refused;
// closing it here is what stops a stale "New device" window from sitting over
// the chat.
func (g *gui) confirmFirstContact(c e2e.FirstContact) bool {
	answer := make(chan bool, 1)

	var box *dialog.ConfirmDialog

	fyne.Do(func() {
		g.setStatus("A new device wants to join — code " + c.Code)
		box = g.showCopyableConfirm("New device", firstContactText(c), "Accept", "Refuse", func(ok bool) {
			// One answer per question: a second tap on a dialog that is being
			// taken away must not send on a channel nobody is reading.
			select {
			case answer <- ok:
			default:
			}
		})
	})

	select {
	case ok := <-answer:
		if ok {
			g.setStatus("Accepted " + contactName(c) + " — its device key is pinned")

			return true
		}

		g.setStatus("Refused " + contactName(c) + " — nothing was pinned")

		return false
	case <-time.After(e2e.DefaultConfirmTimeout):
		fyne.Do(func() {
			if box != nil {
				box.Hide()
			}

			g.setStatus("A new device was not confirmed in time — refused, nothing pinned")
		})

		return false
	}
}
