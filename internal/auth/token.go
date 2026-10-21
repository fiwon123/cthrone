// Package auth holds the shared secret that the websocket transport uses to
// decide whether a client may join a session.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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
	SourceGenerated   = "generated"
	SourceNone        = "none"
)

// Token is a secret and where it came from.
type Token struct {
	Value  string
	Source string
}

// Resolve returns the secret to use and where it came from.
//
// The flag wins over the environment, and the environment over the file: the
// more explicit the source, the more it was meant for this run. A file that
// does not exist is not an error — it means there is no secret yet, which
// the caller may want to create.
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

	return Token{Value: secret, Source: SourceFile}, nil
}

// Generate returns a new random secret.
func Generate() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a token: %w", err)
	}

	return hex.EncodeToString(buf), nil
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

// WriteFile stores the secret in the file, creating the directory. The file
// is written for its owner only: a secret that every process on the machine
// can read is not a secret.
func WriteFile(path, value string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// Equal compares two secrets without leaking how much of them matched.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
