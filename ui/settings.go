package ui

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/discovery"
)

// defaultPort is the port both transports use unless another is given.
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

// setupView records which Setup rows apply to the configuration in force.
type setupView struct {
	// Options is the "More options" button. It belongs to the websocket
	// transport, whose advanced rows it opens; the NATS rows are always shown.
	Options bool

	// SharedSecret, Fingerprint and BindAddress are the advanced websocket
	// rows, shown only once "More options" is open. Fingerprint joins them
	// only while TLS is on — a certificate fingerprint means nothing to a
	// plain ws:// connection.
	SharedSecret bool
	Fingerprint  bool
	BindAddress  bool

	// Insecure is the "Skip the certificate check" row: it is meaningless
	// without TLS.
	Insecure bool

	// NATSBroker and NATSSubject describe the broker, so only the NATS
	// transport uses them. The websocket transport has an address and a port
	// instead.
	NATSBroker  bool
	NATSSubject bool
}

// setupVisibility decides which Setup rows apply to the current mode.
//
// useNATS is the transport in force, tls whether encryption is on, and
// moreOptions whether the advanced websocket rows have been opened.
func setupVisibility(useNATS, tls, moreOptions bool) setupView {
	if useNATS {
		return setupView{Insecure: tls, NATSBroker: true, NATSSubject: true}
	}

	return setupView{
		Options:      true,
		SharedSecret: moreOptions,
		Fingerprint:  moreOptions && tls,
		BindAddress:  moreOptions,
		Insecure:     tls,
	}
}

// setupHint is the line under the actions. The two transports are configured
// differently enough that one sentence cannot serve both: a websocket chat
// pairs two devices — by device key, or with a shared secret — and can pin a
// certificate, while the NATS path also needs a broker it is allowed to join.
func setupHint(useNATS bool) string {
	if useNATS {
		return "NATS: Host or Connect joins the broker, pairing by device key or with the shared secret; the broker's own credentials come from NATS_USER and NATS_PASSWORD."
	}

	return "Fast: select Host to start, or pick a device from Devices and Connect. The connection is encrypted and the certificate is pinned on first contact; with no shared secret the devices also pair by key — compare the fingerprint each shows."
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
