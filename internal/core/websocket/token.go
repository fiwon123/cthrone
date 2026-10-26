package websocketcore

import (
	"fmt"

	"github.com/fiwon123/cthrone/internal/auth"
)

// tokenConfig says where the shared secret may come from.
type tokenConfig struct {
	flagValue string
	filePath  string
}

// hostToken returns the secret the host requires. When there is none, it
// generates one and stores it: the two devices need the same secret, and
// the only way to get it is to read it.
func hostToken(cfg tokenConfig) (string, error) {
	token, err := auth.Resolve(cfg.flagValue, cfg.filePath)
	if err != nil {
		return "", err
	}

	if token.Value != "" {
		return token.Value, nil
	}

	file, err := tokenFile(cfg.filePath)
	if err != nil {
		return "", err
	}

	secret, err := auth.Generate()
	if err != nil {
		return "", err
	}

	if err := auth.WriteFile(file, secret); err != nil {
		return "", err
	}

	fmt.Println("Generated a shared secret in", file)
	fmt.Println("Copy it to the other device, or point that device at the same file.")

	return secret, nil
}

// clientToken returns the secret the client sends. A client with no secret
// cannot join a host that requires one; the host refuses the connection and
// says so.
func clientToken(cfg tokenConfig) (string, error) {
	token, err := auth.Resolve(cfg.flagValue, cfg.filePath)
	if err != nil {
		return "", err
	}

	return token.Value, nil
}

// tokenFile returns the file the secret is read from, or the default one
// when no file was given.
func tokenFile(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	file, err := auth.DefaultFile()
	if err != nil {
		return "", err
	}

	return file, nil
}
