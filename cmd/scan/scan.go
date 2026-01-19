package scancmd

import (
	core "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

// Cmd represents the scan command
var Cmd = &cobra.Command{
	Use:   "scan",
	Short: "scan available IPs",
	Long: `scan available local IPs starting with '192.168.'

Usage:
	cthrone scan
`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")

		app := app.New(port)

		core.Scan(app)
	},
}

func init() {

}
