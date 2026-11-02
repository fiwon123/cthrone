//go:build gui

package ui

import (
	"hash/fnv"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// senderPalette is the set of theme colours a sender's name can take.
//
// Theme colours rather than fixed RGB: they have to be legible on both the light
// and the dark theme, and Fyne resolves them from the one in force.
var senderPalette = []fyne.ThemeColorName{
	theme.ColorNamePrimary,
	theme.ColorNameSuccess,
	theme.ColorNameWarning,
	theme.ColorNameError,
}

// senderColour is the colour for a name: the name decides, so a device is the
// same colour in every window, on every device, and after a restart (#346).
func senderColour(name string) color.Color {
	if name == "" {
		name = "You"
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))

	return theme.Color(senderPalette[hash.Sum32()%uint32(len(senderPalette))])
}

// setSenderLine writes a row's sender line: the name in its colour, or hidden
// when the row has no sender to show.
func (g *gui) setSenderLine(line *canvas.Text, m *chatMessage) {
	name := m.senderDisplay()

	line.Text = name
	line.Color = senderColour(name)
	line.Show()
	line.Refresh()
}
