//go:build gui

package ui

import (
	"hash/fnv"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/ui/widgets"
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
	return theme.Color(senderPalette[hashedSender(name)])
}

// setSenderLine writes a row's sender line: the name in its colour, or hidden
// when the row has no sender to show.
//
// selected prefixes a check and takes the primary colour, which is how a reader
// sees which rows a range covers without a second widget in every row (#469).
func (g *gui) setSenderLine(line *widgets.Text, m *chatMessage, selected bool) {
	name := m.senderDisplay()

	if selected {
		name = "✓ " + name
	}

	line.SetText(name)
	line.SetImportance(senderImportance(m.senderDisplay(), selected))
}

// paletteImportance is each palette colour as a label can express it: a themed
// label takes an importance, not a colour, and these are the theme colours that
// importance resolves to.
var paletteImportance = map[fyne.ThemeColorName]widget.Importance{
	theme.ColorNamePrimary: widget.HighImportance,
	theme.ColorNameSuccess: widget.SuccessImportance,
	theme.ColorNameWarning: widget.WarningImportance,
	theme.ColorNameError:   widget.DangerImportance,
}

// senderImportance is the colour for a name: the name decides, so a device is
// the same colour in every window, on every device, and after a restart (#346).
// A selected row takes the primary colour, which is how a reader sees which
// rows a range covers (#469).
func senderImportance(name string, selected bool) widget.Importance {
	if selected {
		return widget.HighImportance
	}

	return paletteImportance[senderPalette[hashedSender(name)]]
}

// hashedSender is the palette entry a name hashes to.
func hashedSender(name string) int {
	if name == "" {
		name = "You"
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))

	return int(hash.Sum32() % uint32(len(senderPalette)))
}
