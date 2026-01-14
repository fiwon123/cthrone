package connectcmd

import (
	"github.com/fiwon123/cthrone/internal/core"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag string

// Cmd represents the connect command
var Cmd = &cobra.Command{
	Use:   "connect",
	Short: "connect to a device",
	Long:  `connect to a device"`,
	Run: func(cmd *cobra.Command, args []string) {
		app := app.New(8080)

		if natsFlag != "" {
			natscore.Connect(natsFlag, app)
		} else {
			core.Connect("192.168.0.1", app)
		}
	},
}

func init() {
	Cmd.Flags().StringVar(&natsFlag, "nats", "", "host a nats server")
}
