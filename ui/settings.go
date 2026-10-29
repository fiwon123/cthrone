package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// defaultPort is the port both transports use unless another is given.
const defaultPort = 8080

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

	return "Fast: select Host to start, or pick a device from Devices and Connect. With no shared secret the devices pair on first contact — compare the fingerprint each shows."
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
