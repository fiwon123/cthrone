package ui

import "fyne.io/fyne/v2"

// The room a dialog gives its message before it scrolls. A fingerprint, a
// host:port and a short stack trace all fit; a long one scrolls instead of
// growing the window past the screen.
const (
	dialogTextWidth  = 420
	dialogTextHeight = 180
	dialogMargin     = 64
)

// The smallest message box worth showing: below this the text is a slit rather
// than a paragraph, and the dialog's buttons are more useful than the message.
const (
	dialogTextMinWidth  = 120
	dialogTextMinHeight = 80
)

// dialogTextSize is the size of a dialog's message box on a canvas of size
// canvas.
//
// The preferred size is used when the window has room for it, and the box is
// shrunk to the window when it does not: a phone is narrower than the width a
// fingerprint wraps at, and a dialog larger than its canvas is a dialog whose
// buttons are off the screen. A zero canvas — the size before the window is
// drawn, or a test that does not care — means "no limit", so the preferred size
// comes back unchanged.
func dialogTextSize(canvas fyne.Size) fyne.Size {
	size := fyne.NewSize(dialogTextWidth, dialogTextHeight)
	if canvas.Width <= 0 || canvas.Height <= 0 {
		return size
	}

	if canvas.Width < size.Width+dialogMargin {
		size.Width = canvas.Width - dialogMargin
	}

	if canvas.Height < size.Height+dialogMargin*2 {
		size.Height = canvas.Height - dialogMargin*2
	}

	if size.Width < dialogTextMinWidth {
		size.Width = dialogTextMinWidth
	}

	if size.Height < dialogTextMinHeight {
		size.Height = dialogTextMinHeight
	}

	return size
}
