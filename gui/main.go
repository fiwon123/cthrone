// Command gui is the window binary, cthrone_gui.
//
// It is a window, not a command line: it starts the window whatever the
// arguments are. The binary used to be the command tree built with the `gui`
// tag, which meant a double-click depended on the CLI reaching its
// no-subcommand path — the report was "it opens only when I type
// `cthrone_gui.exe ui`" (#382). A window has no subcommands, so nothing is
// parsed here and nothing can stand between a click and the window.
//
// The CLI keeps its own entry point (`main.go` and `cthrone ui`), and the
// archive ships both binaries.
package main

import "github.com/fiwon123/cthrone/ui"

// runWindow is the seam a test uses. A package's main is otherwise
// unreachable from a test, and the defect this file exists to prevent is
// "the window did not start" — which is exactly what a test must be able to
// see.
var runWindow = ui.Run

func main() {
	runWindow()
}
