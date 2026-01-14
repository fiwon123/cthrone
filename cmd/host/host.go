package hostcmd

import (
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag string

// Cmd represents the host command
var Cmd = &cobra.Command{
	Use:   "host",
	Short: "host server",
	Long:  `host server`,
	Run: func(cmd *cobra.Command, args []string) {
		app := app.New(8080)

		if natsFlag != "" {
			natscore.Host(natsFlag, app)
		}
	},
}

func init() {
	Cmd.Flags().StringVar(&natsFlag, "nats", "", "host a nats server")
}
