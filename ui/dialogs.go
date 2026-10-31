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
// text, so selection is off there and copying goes through a tap on the row or
// an explicit Copy button.
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
