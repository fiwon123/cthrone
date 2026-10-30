//go:build gui

package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// selectableLabel is the label this app uses wherever the text is something a
// user might need to copy — a message, an address, a fingerprint, an error.
//
// A Fyne label is inert by default: it cannot be selected, so the only way to
// move its text is to retype it. Selectable turns the same widget into
// drag-selectable text with the platform's copy shortcut, which is what a bug
// report needs and what a phone has no keyboard to substitute for.
func selectableLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	label.Selectable = true

	return label
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
	scroll := container.NewVScroll(selectableLabel(message))
	scroll.SetMinSize(g.fitDialogText())

	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), nil)
	copyBtn.OnTapped = func() {
		if g.app != nil {
			g.app.Clipboard().SetContent(message)
		}

		copyBtn.SetText("Copied")
	}

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
// and the dialog has a confirm and a cancel.
func (g *gui) showCopyableConfirm(title, message, confirm string, action func(bool)) {
	if g.win == nil {
		return
	}

	dialog.NewCustomConfirm(title, confirm, "Cancel", g.copyableBody(message), action, g.win).Show()
}
