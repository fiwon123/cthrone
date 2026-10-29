package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	// Security is the anonymous-client row and its "More options" button. It
	// belongs to the websocket transport: a NATS broker authenticates its
	// peers itself.
	Security bool

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
		Security:     true,
		SharedSecret: moreOptions,
		Fingerprint:  moreOptions && tls,
		BindAddress:  moreOptions,
		Insecure:     tls,
	}
}

// setupHint is the line under the actions. The two transports are configured
// differently enough that one sentence cannot serve both: a websocket chat
// works anonymously by default, while the NATS path is only as authenticated
// as the broker it joins.
func setupHint(useNATS bool) string {
	if useNATS {
		return "NATS: Host or Connect joins the broker. The broker's credentials come from NATS_USER and NATS_PASSWORD."
	}

	return "Fast: select Host to start, or pick a device from Devices and Connect. Anonymous is on by default for easy local chat."
}

// resolveConfigDir returns the directory the app keeps its token and its TLS
// material in.
//
// On the desktop it is the same ~/.config/cthrone the CLI uses, so a secret
// the GUI generates is the one the CLI already knows. On mobile there is no
// ~/.config to rely on, so the app's own storage directory — which the
// platform always provides — comes first.
func resolveConfigDir(mobile bool, storageRoot string) string {
	if !mobile {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "cthrone")
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
