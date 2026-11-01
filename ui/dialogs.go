//go:build gui

package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// label is the app's text helper: a message, an address, a fingerprint, an
// error.
//
// A Fyne label is inert by default: it cannot be selected, so the only way to
// move its text is to retype it. On a desktop Selectable turns the same widget
// into drag-selectable text with the platform's copy shortcut, which is what a
// bug report needs. On a touch screen the same drag has to reach the scroll
// container underneath, or a pane cannot be scrolled by starting the gesture on
// text — so a line a user reads on a phone is made with text below, which takes
// the selection on a long press and leaves the drag to the pane.
//
// It delegates so the one definition of "selectable" lives in ui/widgets,
// beside the caption and form helpers that need the same behaviour.
func (g *gui) label(text string) *widget.Label {
	return widgets.Label(text, !g.mobile)
}

// heading is label for a tab's or a section's title.
func (g *gui) heading(text string) *widget.Label {
	return widgets.Heading(text, !g.mobile)
}

// text is label for text a touch screen must be able to select on a long press:
// the transcript, the status and hint lines, and the read-only values on Setup
// and About.
//
// It is not the default for every label because it costs a tap callback: on a
// phone it is the object the driver hands a tap to, so a line whose tap means
// something has to say what that is. A heading or a caption nobody selects
// keeps the plain label.
func (g *gui) text(text string) *widgets.Text {
	return widgets.NewText(text, g.mobile)
}

// textHeading is text in bold: the same rule, for a title.
func (g *gui) textHeading(text string) *widgets.Text {
	return widgets.NewHeading(text, g.mobile)
}

// fitDialogText is the size of a dialog's message box, kept inside the window.
func (g *gui) fitDialogText() fyne.Size {
	if g.win == nil {
		return dialogTextSize(fyne.Size{})
	}

	return dialogTextSize(g.win.Canvas().Size())
}

// copyableBody is the message area both dialog forms share: the text in a
// scrollable, selectable label with a Copy button under it.
//
// The button exists because selection alone is awkward in a dialog — the text
// the user wants is usually all of it — and because a phone's copy gesture
// needs a long press the message does not advertise.
func (g *gui) copyableBody(message string) fyne.CanvasObject {
	scroll := container.NewVScroll(g.label(message))
	scroll.SetMinSize(g.fitDialogText())

	copyBtn := widgets.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), nil, g.copyLabel)
	copyBtn.SetOnTapped(func() {
		if g.app != nil {
			g.app.Clipboard().SetContent(message)
		}

		copyBtn.SetText("Copied")
	})

	return container.NewBorder(nil, container.NewHBox(layout.NewSpacer(), copyBtn), nil, nil, scroll)
}

// showCopyable shows a message whose text can be selected, with a button that
// copies all of it.
//
// This replaces dialog.ShowInformation and dialog.ShowError, whose body is an
// inert label: the same message, but one that has to be retyped by hand out of
// a screenshot.
func (g *gui) showCopyable(title, message string) {
	if g.win == nil {
		return
	}

	dialog.NewCustom(title, "Close", g.copyableBody(message), g.win).Show()
}

// showCopyableConfirm is showCopyable's asking form: the message can be copied,
// and the dialog has a confirm and a cancel button whose labels say what the
// answer means on this screen — "Delete"/"Cancel" for a file, "Accept"/"Refuse"
// for a device.
//
// The dialog is returned so a caller that is waiting on the answer can take it
// off the screen when the question has expired.
func (g *gui) showCopyableConfirm(title, message, confirm, cancel string, action func(bool)) *dialog.ConfirmDialog {
	if g.win == nil {
		return nil
	}

	box := dialog.NewCustomConfirm(title, confirm, cancel, g.copyableBody(message), action, g.win)
	box.Show()

	return box
}
