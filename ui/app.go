//go:build gui

package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Run starts the Fyne GUI.
func Run() {
	a := app.New()
	w := a.NewWindow("cthrone")
	w.SetContent(container.NewVBox(
		widget.NewLabel("cthrone - device chat"),
		widget.NewLabel("Use host/connect via CLI or this UI"),
	))
	w.Resize(fyne.NewSize(600, 400))
	w.ShowAndRun()
}
