package hostcmd

import (
	"errors"
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/spf13/cobra"
)

var natsFlag bool

var natsURLFlag string

var bindAddressFlag string

var allowAnonymousFlag bool

var nameFlag string

// Cmd represents the host command
var Cmd = &cobra.Command{
	Use:   "host",
	Short: "host device",
	Long: `host device is the main device to receive message.

On websocket connection devices can send and receive message from host or connect device
On nats connection only host receive message and only connect device send message`,
	Run: func(cmd *cobra.Command, args []string) {
		if allowAnonymousFlag {
			log.Fatal(errors.New("--allow-anonymous has been retired: every session is encrypted and authenticated, by device key or shared secret"))
		}

		port, _ := cmd.Flags().GetInt("port")
		secret, _ := cmd.Flags().GetString("token")
		secretFile, _ := cmd.Flags().GetString("token-file")
		tlsFlag, _ := cmd.Flags().GetBool("tls")
		noTLSFlag, _ := cmd.Flags().GetBool("no-tls")
		fingerprint, _ := cmd.Flags().GetString("cert-fingerprint")
		acceptAny, _ := cmd.Flags().GetBool("insecure")

		// The websocket transport is encrypted unless --no-tls says
		// otherwise; the NATS one is decided by its broker URL.
		tlsEnabled, err := auth.TransportTLS(tlsFlag, noTLSFlag)
		if err != nil {
			log.Fatal(err)
		}

		app := app.New(port)
		app.Host = bindAddressFlag
		app.Name = nameFlag
		// Pairing pins a peer by name, so a run without --name still needs one.
		app.Name = app.ResolveName()

		tlsConf := auth.TLS{
			Enabled:     tlsEnabled,
			Fingerprint: fingerprint,
			Insecure:    acceptAny,
		}

		opts := pairing.Options{FlagToken: secret, TokenFile: secretFile}

		if natsFlag {
			natscore.Host(args, app, opts, natsURLFlag, tlsConf)

			return
		}

		if err := websocketcore.Host(app, opts, tlsConf); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
	Cmd.Flags().StringVar(&bindAddressFlag, "host", "", "the interface to bind to (default: every interface, so another device can reach it)")
	Cmd.Flags().BoolVar(&allowAnonymousFlag, "allow-anonymous", false, "retired: every session is encrypted and authenticated")
	Cmd.Flags().StringVar(&nameFlag, "name", "", "device name announced on the network")
}
