//go:build gui

package cmd

import (
	"fmt"

	"github.com/fiwon123/cthrone/ui"
	"github.com/spf13/cobra"
)

func init() {
	// If no subcommand is specified, launch the GUI.
	origRun := rootCmd.Run
	_ = origRun
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		if checkVersion {
			fmt.Println(Version)
			return
		}
		if len(args) > 0 {
			_ = cmd.Help()
			return
		}
		ui.Run()
	}
}
