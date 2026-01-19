package hostcmd

import (
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag bool

// Cmd represents the host command
var Cmd = &cobra.Command{
	Use:   "host",
	Short: "host server",
	Long:  `host server`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")

		app := app.New(port)

		if natsFlag {
			natscore.Host(args, app)
		} else {
			websocketcore.Host(app)
		}
	},
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "host a nats server")
}
