//go:build !gui

package ui

import "fmt"

// Run starts the Fyne GUI.
func Run() {
	fmt.Println("GUI not built. Build with -tags gui and required system libraries.")
}
