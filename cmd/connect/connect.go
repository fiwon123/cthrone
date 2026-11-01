package connectcmd

import (
	"errors"
	"fmt"
	"log"

	"github.com/fiwon123/cthrone/internal/auth"
	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/spf13/cobra"
)

var natsFlag bool

var bluetoothFlag bool

var natsURLFlag string

var nameFlag string

// connectWebsocket is the websocket client the command runs. It is a variable
// so a test can see the TLS setting the flags resolved to without a host to
// dial.
var connectWebsocket = websocketcore.Connect

// connectBluetooth is the Bluetooth client the command runs. It is a variable
// so a test can see the flags it resolves to without a radio.
var connectBluetooth = bluetoothcore.Connect

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
		tlsFlag, _ := cmd.Flags().GetBool("tls")
		noTLSFlag, _ := cmd.Flags().GetBool("no-tls")
		fingerprint, _ := cmd.Flags().GetString("cert-fingerprint")
		acceptAny, _ := cmd.Flags().GetBool("insecure")

		// The websocket transport is encrypted unless --no-tls says
		// otherwise; the NATS one is decided by its broker URL.
		tlsConf, err := auth.TLSFromFlags(tlsFlag, noTLSFlag, fingerprint, acceptAny)
		if err != nil {
			log.Fatal(err)
		}

		app := app.New(port)
		app.Name = nameFlag
		// Pairing pins a peer by name, so a run without --name still needs one.
		app.Name = app.ResolveName()

		opts := pairing.Options{FlagToken: secret, TokenFile: secretFile}

		// Same line as the host: which credential this run speaks, and where
		// it came from. A left-over token file is otherwise invisible until a
		// key-paired peer is refused with no explanation.
		fmt.Println("Credential:", pairing.DescribeOptions(opts))

		if notice := pairing.LegacyTokenFileNotice(opts); notice != "" {
			fmt.Println(notice)
		}

		if err := transportConflict(bluetoothFlag, natsFlag); err != nil {
			log.Fatal(err)
		}

		if bluetoothFlag {
			// No port and no certificate: the argument names a Bluetooth
			// device — an address, a name, or nothing for the first one in
			// range.
			if err := connectBluetooth(args, app, opts); err != nil {
				log.Fatal(err)
			}

			return
		}

		if natsFlag {
			natscore.Connect(args, app, opts, natsURLFlag, tlsConf)

			return
		}

		if err := connectWebsocket(args, app, opts, tlsConf); err != nil {
			log.Fatal(err)
		}
	},
}

// transportConflict refuses two switches that name different carriers. A run
// that passed both has not said which transport to use, and choosing one for it
// would be a guess; the transport is the one thing the command cannot infer.
//
// It is a function so a test can read the refusal: log.Fatal ends the process,
// and a test that calls it verifies nothing but its own exit.
func transportConflict(bluetooth, nats bool) error {
	if bluetooth && nats {
		return errors.New("--bluetooth and --nats select different transports; name one")
	}

	return nil
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().BoolVar(&bluetoothFlag, "bluetooth", false, "connect over Bluetooth instead of the websocket (Linux only)")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
	Cmd.Flags().StringVar(&nameFlag, "name", "", "device name the host shows for this device")
}
