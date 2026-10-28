//go:build gui

package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed logo.png
var logoData []byte

func appIcon() fyne.Resource {
	if len(logoData) == 0 {
		return nil
	}
	return fyne.NewStaticResource("logo.png", logoData)
}
