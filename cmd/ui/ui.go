package uicmd

import (
	"github.com/fiwon123/cthrone/ui"
	"github.com/spf13/cobra"
)

// Cmd represents the ui command
var Cmd = &cobra.Command{
	Use:   "ui",
	Short: "launch the GUI",
	Long:  `Launch the cthrone GUI for Windows, Linux, macOS, Android and iOS (build documented).`,
	Run: func(cmd *cobra.Command, args []string) {
		ui.Run()
	},
}

// Run starts the GUI. The root command calls it when it is given no
// subcommand, which is what a double-click on cthrone_gui does: that binary is
// this same command built with -tags gui and -H=windowsgui, so there is no
// console left to explain a binary that starts nothing.
func Run() {
	ui.Run()
}

func init() {}
