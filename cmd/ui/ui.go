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

func init() {}
