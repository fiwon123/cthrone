// Package auth holds the shared secret a session can authenticate with, and the
// TLS material that pins a host.
//
// The secret is an alternative to the device-key model: this package reads one
// when it is configured and compares it in constant time. It never makes one —
// a generated secret used to be the host's default, and now a device pairs by
// its own key unless a secret is supplied.
package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvToken is the environment variable the secret can come from. The
	// tool reads it; it never writes it.
	EnvToken = "CTHRONE_TOKEN"

	// HeaderName is the header the client sends the secret in. A header and
	// not a query parameter: a URL ends up in proxy logs and in shell
	// history, and a secret should not.
	HeaderName = "X-Cthrone-Token"
)

// Where a secret came from.
const (
	SourceFlag        = "flag"
	SourceEnvironment = "environment"
	SourceFile        = "file"
	SourceNone        = "none"
)

// Token is a secret and where it came from.
type Token struct {
	Value  string
	Source string

	// Path is the file the secret was read from, and only set when Source is
	// SourceFile. It is not secret — the path is not the key — and it is what
	// lets a status line say which file is in charge, and a "stop using this"
	// action delete the right one.
	Path string
}

// Resolve returns the secret to use and where it came from.
//
// The flag wins over the environment, and the environment over the file: the
// more explicit the source, the more it was meant for this run. A file that
// does not exist is not an error — it means no secret is configured, which is
// the ordinary case now that a device pairs by its own key.
func Resolve(flagValue, filePath string) (Token, error) {
	if flagValue != "" {
		return Token{Value: flagValue, Source: SourceFlag}, nil
	}

	if value := os.Getenv(EnvToken); value != "" {
		return Token{Value: value, Source: SourceEnvironment}, nil
	}

	if filePath == "" {
		return Token{Source: SourceNone}, nil
	}

	value, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{Source: SourceNone}, nil
		}

		return Token{}, fmt.Errorf("reading the token file %s: %w", filePath, err)
	}

	secret := strings.TrimSpace(string(value))
	if secret == "" {
		return Token{Source: SourceNone}, nil
	}

	return Token{Value: secret, Source: SourceFile, Path: filePath}, nil
}

// DefaultFile returns the path of the default token file: the token file
// inside the user's configuration directory.
func DefaultFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the configuration directory: %w", err)
	}

	return filepath.Join(dir, "cthrone", "token"), nil
}

// Equal compares two secrets without leaking how much of them matched.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
