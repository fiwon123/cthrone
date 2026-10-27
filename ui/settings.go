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
