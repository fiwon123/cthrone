package natscore

import "github.com/fiwon123/cthrone/internal/auth"

// sharedSecret resolves the shared secret the same way the websocket path
// does. One secret protects both transports: the broker's credentials say who
// may publish, and this secret says who may read.
func sharedSecret(flagToken, tokenFile string) (string, error) {
	token, err := auth.Resolve(flagToken, tokenFile)
	if err != nil {
		return "", err
	}

	return token.Value, nil
}
