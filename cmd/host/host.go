package hostcmd

import (
	"log"

	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag bool

var natsURLFlag string

var bindAddressFlag string

var allowAnonymousFlag bool

// Cmd represents the host command
var Cmd = &cobra.Command{
	Use:   "host",
	Short: "host device",
	Long: `host device is the main device to receive message.

On websocket connection devices can send and receive message from host or connect device
On nats connection only host receive message and only connect device send message`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		secret, _ := cmd.Flags().GetString("token")
		secretFile, _ := cmd.Flags().GetString("token-file")
		tlsEnabled, _ := cmd.Flags().GetBool("tls")

		app := app.New(port)
		app.Host = bindAddressFlag
		app.AllowAnonymous = allowAnonymousFlag

		if natsFlag {
			natscore.Host(args, app, natsURLFlag)

			return
		}

		if err := websocketcore.Host(app, secret, secretFile, websocketcore.TLS{Enabled: tlsEnabled}); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
	Cmd.Flags().StringVar(&bindAddressFlag, "host", "", "the interface to bind to (default: every interface, so another device can reach it)")
	Cmd.Flags().BoolVar(&allowAnonymousFlag, "allow-anonymous", false, "let clients join without the shared secret")
}
