package ui

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fiwon123/cthrone/internal/auth"
	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
	"github.com/fiwon123/cthrone/internal/discovery"
)

// defaultPort is the port the network transports use unless another is given.
// The radio has none: a Bluetooth link has no address to type.
const defaultPort = 8080

// labelConnectTo names the Setup row that holds the host to dial.
//
// It is not this device's address: that one is shown read-only while hosting,
// by thisDeviceText, because the other device's scan is what finds it. The
// label says what the field does, so a user who is hosting does not read an
// empty box as "my address".
const labelConnectTo = "Connect to"

// deviceAddresses lists this device's addresses as host:port pairs — the form
// the other device types, or its scan finds. An empty list returns "", because
// a device with no reachable address has nothing to advertise.
func deviceAddresses(addrs []string, port int) string {
	if len(addrs) == 0 {
		return ""
	}

	parts := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		parts = append(parts, net.JoinHostPort(addr, strconv.Itoa(port)))
	}

	return strings.Join(parts, ", ")
}

// thisDeviceText is the read-only line that tells a hosting device its own
// address: the values the other device's scan will find. A host bound to every
// interface answers on each address this device holds, so each one is listed.
// An empty list means there is nothing to show.
func thisDeviceText(addrs []string, port int) string {
	addresses := deviceAddresses(addrs, port)
	if addresses == "" {
		return ""
	}

	return "This device — " + addresses
}

// parsePort returns the port in text, or defaultPort when text is not a
// usable port. A GUI has no line to print a usage error to, and a port that
// cannot be parsed is better replaced than obeyed.
func parsePort(text string) int {
	port, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || port < 1 || port > 65535 {
		return defaultPort
	}

	return port
}

// subjectOrDefault returns the NATS subject in text, or fallback when the
// field is empty. An empty subject is not a subject the broker accepts.
func subjectOrDefault(text, fallback string) string {
	subject := strings.TrimSpace(text)
	if subject == "" {
		return fallback
	}

	return subject
}

// transportMode names the carrier the Setup tab configures.
//
// The three need different things from the user, which is why this is a mode
// rather than a switch: a websocket needs an address and a port, NATS needs a
// broker and a subject, and the radio needs neither — only a device name, and
// optionally which device to look for.
type transportMode int

const (
	transportWebsocket transportMode = iota
	transportNATS
	transportBluetooth
)

// The transport select's option text. They are constants because the mapping
// runs both ways — the option a user picks is a mode, and a mode has to be
// written back into the select — and two string literals that have to agree are
// a bug waiting for a typo.
const (
	labelWebsocket = "WebSocket (default)"
	labelNATS      = "NATS"
	labelBluetooth = "Bluetooth"
)

// bluetoothSupported is the fact the transport select is built from: the one
// bluetoothcore.Supported() answers for this build.
//
// It is a variable rather than a direct call so a test can build the window for
// a platform this host is not — the same seam hostBluetooth and connectBluetooth
// give the transport tests, for the same reason: the answer belongs to the
// build, and no window here runs on Windows or macOS.
var bluetoothSupported = bluetoothcore.Supported

// transportNames are the option labels for this build, in the order the select
// shows them.
//
// The radio is offered only by a build that has it. The decision used to follow
// the layout — a phone was denied the option because it was a phone — which was
// wrong in both directions: a Windows or macOS desktop offered "Bluetooth
// (Linux)" and could only answer "this build has no Bluetooth transport", while
// a platform that gained the transport would have stayed hidden. An option that
// cannot work reads as a broken feature; its absence reads as a platform
// difference.
func transportNames(bluetooth bool) []string {
	if !bluetooth {
		return []string{labelWebsocket, labelNATS}
	}

	return []string{labelWebsocket, labelNATS, labelBluetooth}
}

// transportOf maps the selected option back to a mode. Anything unrecognised —
// including the empty string a select reports before it has a value — is the
// websocket, which is the default.
func transportOf(selected string) transportMode {
	switch selected {
	case labelNATS:
		return transportNATS
	case labelBluetooth:
		return transportBluetooth
	default:
		return transportWebsocket
	}
}

// transportName is the option text a mode is shown as.
func transportName(mode transportMode) string {
	switch mode {
	case transportNATS:
		return labelNATS
	case transportBluetooth:
		return labelBluetooth
	default:
		return labelWebsocket
	}
}

// setupView records which Setup rows apply to the configuration in force.
type setupView struct {
	// Options is the "More options" button. It opens the advanced rows of the
	// transport selected: the websocket's credential and bind address, or the
	// radio's credential. The NATS rows are always shown.
	Options bool

	// Port is the row both network transports use. The radio has no port to
	// listen on or dial, so the row is absent rather than ignored.
	Port bool

	// SharedSecret, Fingerprint and BindAddress are the advanced rows, shown
	// only once "More options" is open. The credential is the one of the three
	// that every transport has — a session is authenticated whichever carrier
	// it runs on — while Fingerprint (a host certificate) and BindAddress (a
	// listening socket) belong to the websocket alone. Fingerprint joins them
	// only while TLS is on: a certificate fingerprint means nothing to a plain
	// ws:// connection.
	SharedSecret bool
	Fingerprint  bool
	BindAddress  bool

	// Insecure is the "Skip the certificate check" row: it is meaningless
	// without TLS, which the radio and the broker URL do not use.
	Insecure bool

	// NATSBroker and NATSSubject describe the broker, so only the NATS
	// transport uses them. The websocket transport has an address and a port
	// instead, and the radio has neither.
	NATSBroker  bool
	NATSSubject bool
}

// setupVisibility decides which Setup rows apply to the current mode.
//
// mode is the transport in force, tls whether encryption is on, and
// moreOptions whether the advanced websocket rows have been opened.
func setupVisibility(mode transportMode, tls, moreOptions bool) setupView {
	switch mode {
	case transportNATS:
		// A broker is reached, not bound: no port, no address to advertise, no
		// certificate of this device's to pin. The shared secret still applies,
		// because it is the credential, not a property of the carrier.
		return setupView{Port: true, Insecure: tls, NATSBroker: true, NATSSubject: true}

	case transportBluetooth:
		// The radio has no port, no bind address and no certificate. What it
		// does have is the credential: a session on Bluetooth is sealed and
		// authenticated exactly as the other two are.
		return setupView{Options: true, SharedSecret: moreOptions}

	default:
		return setupView{
			Port:         true,
			Options:      true,
			SharedSecret: moreOptions,
			Fingerprint:  moreOptions && tls,
			BindAddress:  moreOptions,
			Insecure:     tls,
		}
	}
}

// setupHint is the line under the actions. The transports are configured
// differently enough that one sentence cannot serve them: a websocket chat
// pairs two devices — by device key, or with a shared secret — and can pin a
// certificate, the NATS path also needs a broker it is allowed to join, and the
// radio needs neither an address nor a broker, only proximity.
func setupHint(mode transportMode) string {
	switch mode {
	case transportNATS:
		return "NATS: Host or Connect joins the broker, pairing by device key or with the shared secret; the broker's own credentials come from NATS_USER and NATS_PASSWORD."
	case transportBluetooth:
		return "Bluetooth: select Host to advertise, or Connect to the first device in range — a device name or address narrows the search. Text only: one packet carries 20 bytes, so a file is impractical."
	default:
		return "Fast: select Host to start, or pick a device from Devices and Connect. The connection is encrypted and the certificate is pinned on first contact; with no shared secret the devices also pair by key — compare the fingerprint each shows."
	}
}

// resolveConfigDir returns the directory the app keeps its token and its TLS
// material in.
//
// On the desktop it is the same directory the CLI resolves — discovery.ConfigDir
// — so a device that runs `cthrone host` in a terminal and `cthrone ui` on the
// same machine presents one device key and one pin store, not two identities
// that refuse each other. On mobile there is no user configuration directory to
// rely on, so the app's own storage directory — which the platform always
// provides — comes first.
func resolveConfigDir(mobile bool, storageRoot string) string {
	if !mobile {
		if dir, err := discovery.ConfigDir(); err == nil {
			return dir
		}
	}

	if storageRoot != "" {
		return storageRoot
	}

	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "cthrone")
	}

	return "."
}

// credentialText is the Setup tab's read row: which credential the next Host or
// Connect will speak, and where it came from.
//
// It never carries the secret itself. A source is what explains the model; the
// value is the one thing that must not sit on a screen.
//
// legacyPath names the token file an older version read without being asked;
// legacy is whether it is still there. That file decides nothing any more —
// a secret is used only when it is typed here or set in the environment — so
// the row says so plainly. A leftover that used to decide the model is exactly
// the thing a user cannot otherwise find: the device joins the shared-secret
// path, refuses a peer that pairs by key, and the only symptom is a connection
// that never completes.
func credentialText(typed string, envSet bool, legacyPath string, legacy bool) string {
	switch {
	case strings.TrimSpace(typed) != "":
		return "Credential: the shared secret typed above — a device that pairs by its key is refused. Clear the field to go back to key pairing."
	case envSet:
		return "Credential: the shared secret in " + auth.EnvToken + " — a device that pairs by its key is refused. Unset it to go back to key pairing."
	case legacy:
		return "Credential: this device's key, pinned by the other device on first contact. The token file " + legacyPath + " from an older version is no longer read — type its secret above to use it, or delete it."
	default:
		return "Credential: this device's key, pinned by the other device on first contact. Compare the two fingerprints the screens show."
	}
}

// secretFileExists reports whether path holds a file at all.
//
// Its content is not read. Nothing decides the credential by a file any more,
// so the line only has to say that a leftover is there, and reading a secret
// to decide what to print is a read that buys nothing.
func secretFileExists(path string) bool {
	if path == "" {
		return false
	}

	_, err := os.Stat(path)

	return err == nil
}
