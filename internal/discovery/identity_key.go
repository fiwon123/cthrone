package discovery

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IdentityKeyFile returns the path of this device's long-term signing key. It
// lives beside the other device state, in the configuration directory.
func IdentityKeyFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "identity.key"), nil
}

// LoadKey returns this device's long-term signing key, generating it on first
// use.
//
// The key is the device's identity: a peer that has seen it once pins it, and
// a different key under the same name is refused. So the file is written for
// its owner only, and a corrupt file is an error rather than a reason to make
// a new identity quietly — a new key is a new device to everyone who pinned
// the old one.
func LoadKey() (ed25519.PrivateKey, error) {
	path, err := IdentityKeyFile()
	if err != nil {
		return nil, err
	}

	stored, err := os.ReadFile(path)
	if err == nil {
		return parseKey(path, stored)
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading the device key %s: %w", path, err)
	}

	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generating a device key: %w", err)
	}

	if err := writeSecret(path, hex.EncodeToString(seed)+"\n"); err != nil {
		return nil, err
	}

	fmt.Println("Generated a device key in", path)
	fmt.Println("It is this device's identity; devices that pair with it pin it.")

	return ed25519.NewKeyFromSeed(seed), nil
}

// parseKey reads the hex seed the key file holds.
func parseKey(path string, stored []byte) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(strings.TrimSpace(string(stored)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s does not hold a device key; delete it only if this device should pair again as a new one", path)
	}

	return ed25519.NewKeyFromSeed(seed), nil
}

// writeSecret creates the directory and stores a secret for its owner only.
func writeSecret(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
