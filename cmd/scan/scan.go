package scancmd

import (
	"github.com/fiwon123/cthrone/internal/core"
	"github.com/spf13/cobra"
)

// Cmd represents the scan command
var Cmd = &cobra.Command{
	Use:   "scan",
	Short: "scan available IPs",
	Long:  `scan available IPs`,
	Run: func(cmd *cobra.Command, args []string) {
		core.Scan()
	},
}

func init() {

}
