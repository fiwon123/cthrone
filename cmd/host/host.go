package hostcmd

import (
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag bool

var natsURLFlag string

// Cmd represents the host command
var Cmd = &cobra.Command{
	Use:   "host",
	Short: "host device",
	Long: `host device is the main device to receive message.

On websocket connection devices can send and receive message from host or connect device
On nats connection only host receive message and only connect device send message`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")

		app := app.New(port)

		if natsFlag {
			natscore.Host(args, app, natsURLFlag)
		} else {
			websocketcore.Host(app)
		}
	},
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
}
