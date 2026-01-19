package connectcmd

import (
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
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
		port, _ := cmd.Flags().GetInt("port")

		app := app.New(port)

		if natsFlag != "" {
			natscore.Connect(natsFlag, app)
		} else {
			websocketcore.Connect(args, app)
		}
	},
}

func init() {
	Cmd.Flags().StringVar(&natsFlag, "nats", "", "host a nats server")
}
