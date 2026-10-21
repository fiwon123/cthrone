package connectcmd

import (
	"log"

	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var natsFlag bool

var natsURLFlag string

var certFingerprintFlag string

var insecureFlag bool

// Cmd represents the connect command
var Cmd = &cobra.Command{
	Use:   "connect",
	Short: "connect to a device",
	Long: `connect to a device to send and receive messages.

On websocket connection devices can send and receive message from host or connect device
On nats connection only host receive message and only connect device send message`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		secret, _ := cmd.Flags().GetString("token")
		secretFile, _ := cmd.Flags().GetString("token-file")
		tlsEnabled, _ := cmd.Flags().GetBool("tls")
		fingerprint, _ := cmd.Flags().GetString("cert-fingerprint")
		insecure, _ := cmd.Flags().GetBool("insecure")

		app := app.New(port)

		if natsFlag {
			natscore.Connect(args, app, natsURLFlag)

			return
		}

		tlsConf := websocketcore.TLS{
			Enabled:     tlsEnabled,
			Fingerprint: fingerprint,
			Insecure:    insecure,
		}

		if err := websocketcore.Connect(args, app, secret, secretFile, tlsConf); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
	Cmd.Flags().StringVar(&certFingerprintFlag, "cert-fingerprint", "", "the fingerprint of the certificate to pin, with --tls")
	Cmd.Flags().BoolVar(&insecureFlag, "insecure", false, "accept any certificate, with --tls; the traffic is still encrypted, but the host is not checked")
}
