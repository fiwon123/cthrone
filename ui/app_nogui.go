//go:build !gui

package ui

import "fmt"

// Run starts the Fyne GUI.
//
// Without the gui build tag there is no toolkit to start, so the honest answer
// is to say which binary can: cthrone_gui ships beside this one in every
// desktop archive, and `make gui-linux` / `make gui-windows` build it.
func Run() {
	fmt.Println("This is the command-line build: it has no window.")
	fmt.Println("The GUI is the cthrone_gui binary in the release archive; build it with -tags gui.")
	fmt.Println("Run \"cthrone --help\" to see the commands.")
}
