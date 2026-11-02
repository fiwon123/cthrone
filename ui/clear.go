//go:build gui

package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// clearAction is the small "×" drawn inside a text field that empties it.
//
// It exists because holding backspace does not work on a phone: Fyne's Android
// keyboard bridge keeps a hidden one-character field as a keystroke proxy and
// rewrites it after every delete, so the first press removes a character and
// every later one is lost. That is the toolkit's to fix (fyne-io/fyne#6648), and
// this is what can be done here: one press to empty a field, on every platform
// (#290).
func clearAction(entry *widget.Entry) fyne.CanvasObject {
	button := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		entry.SetText("")
	})

	// Low importance keeps it flat: an entry is not a place for a raised button.
	button.Importance = widget.LowImportance

	return button
}

// withClear gives a field the clear action, so a caller states the intent rather
// than remembering the field's name. It returns the entry so it can be used in
// the expression that creates the widget.
func withClear(entry *widget.Entry) *widget.Entry {
	entry.ActionItem = clearAction(entry)

	return entry
}
