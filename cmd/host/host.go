package hostcmd

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/chat"
	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var natsFlag bool

var bluetoothFlag bool

var natsURLFlag string

var bindAddressFlag string

var allowAnonymousFlag bool

var nameFlag string

// hostWebsocket is the websocket host the command starts. It is a variable so
// a test can see the TLS setting the flags resolved to without opening a
// listener: the encrypted default is the kind of wiring a test that only
// checks flag registration would leave unproven.
var hostWebsocket = websocketcore.Host

// hostBluetooth is the Bluetooth host the command starts. It is a variable for
// the same reason: a test cannot open a radio.
var hostBluetooth = bluetoothcore.Host

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
		tlsConf, err := auth.TLSFromFlags(tlsFlag, noTLSFlag, fingerprint, acceptAny)
		if err != nil {
			log.Fatal(err)
		}

		app := app.New(port)
		app.Host = bindAddressFlag
		app.Name = nameFlag
		// Pairing pins a peer by name, so a run without --name still needs one.
		app.Name = app.ResolveName()

		opts := pairing.Options{FlagToken: secret, TokenFile: secretFile}

		// Name the credential and where it came from before the transport
		// starts. A token file named on the command line switches the device
		// into the shared-secret model, which refuses a peer that pairs by
		// key, and nothing on screen used to say so.
		fmt.Println("Credential:", pairing.DescribeOptions(opts))

		// The file an older version read without being asked is not read any
		// more. Saying so is what turns a connection that suddenly asks for a
		// secret into an explained change.
		if notice := pairing.LegacyTokenFileNotice(opts); notice != "" {
			fmt.Println(notice)
		}

		if err := transportConflict(bluetoothFlag, natsFlag); err != nil {
			log.Fatal(err)
		}

		if bluetoothFlag {
			// Bluetooth needs no port and no certificate: the radio is the
			// link, and the end-to-end layer seals it as it seals the others.
			if err := hostBluetooth(app, opts, firstContactGate()); err != nil {
				log.Fatal(err)
			}

			return
		}

		if natsFlag {
			natscore.Host(args, app, opts, natsURLFlag, tlsConf)

			return
		}

		if err := hostWebsocket(app, opts, tlsConf, firstContactGate()); err != nil {
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

// firstContactGate is how a new device is let in: a question on the terminal.
//
// A host with no terminal attached has nobody to ask, so it accepts and pins a
// first contact the way it did before the code existed, and says so when the
// device arrives. Refusing every new device would make a headless host
// unusable, and there is no person at such a host to compare a code with
// anyway. The question is asked only when a device that is not already pinned
// turns up, and not at all when a shared secret is configured: the secret path
// has no first-contact code to compare.
func firstContactGate() websocketcore.Confirm {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return chat.AcceptFirstContact(os.Stdout)
	}

	return chat.ConfirmFirstContact(os.Stdin, os.Stdout)
}

func init() {
	Cmd.Flags().BoolVar(&natsFlag, "nats", false, "use the NATS transport instead of the websocket one")
	Cmd.Flags().BoolVar(&bluetoothFlag, "bluetooth", false, "serve over Bluetooth instead of the websocket (Linux only, one device at a time)")
	Cmd.Flags().StringVar(&natsURLFlag, "nats-url", natscore.DefaultBrokerURL, "the NATS broker to connect to")
	Cmd.Flags().StringVar(&bindAddressFlag, "host", "", "the interface to bind to (default: every interface, so another device can reach it)")
	Cmd.Flags().BoolVar(&allowAnonymousFlag, "allow-anonymous", false, "retired: every session is encrypted and authenticated")
	Cmd.Flags().StringVar(&nameFlag, "name", "", "device name announced on the network")
}
