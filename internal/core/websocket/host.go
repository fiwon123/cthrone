package websocketcore

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
)

// host is the function that serves the chat endpoint. It is a variable so a
// test can replace it: serving means listening on a port, and a test cannot
// do that.
var host = websockethandler.Host

// TLS says how the websocket transport is encrypted.
type TLS struct {
	// Enabled makes the host serve https and the client expect it.
	Enabled bool

	// CertFile and KeyFile are where the host keeps its certificate. They
	// are empty by default, which means the files in the user's
	// configuration directory.
	CertFile string
	KeyFile  string

	// Fingerprint is the certificate the client pins. It is checked
	// instead of a certificate authority: the host's certificate is
	// self-signed, so there is no authority to check it against.
	Fingerprint string

	// Insecure makes the client accept any certificate. It is what the
	// flag says, and it is why it has to be asked for.
	Insecure bool
}

// Host open connection
func Host(app *app.Data, flagToken, tokenFile string, tlsConf TLS) error {
	token := ""
	if !app.AllowAnonymous {
		var err error

		token, err = hostToken(tokenConfig{flagValue: flagToken, filePath: tokenFile})
		if err != nil {
			return err
		}
	}

	certFile, keyFile := "", ""
	if tlsConf.Enabled {
		var err error

		certFile, keyFile, err = hostCertificate(tlsConf.CertFile, tlsConf.KeyFile)
		if err != nil {
			return err
		}
	}

	return host(app.Port, app.Host, token, app.AllowAnonymous, certFile, keyFile)
}

// hostCertificate returns the certificate and the key the server uses. It
// generates them the first time and keeps them afterwards: a client that
// pinned the fingerprint must not find a different certificate tomorrow.
func hostCertificate(configuredCert, configuredKey string) (string, string, error) {
	certFile, keyFile, err := auth.DefaultCertificateFiles()
	if err != nil {
		return "", "", err
	}

	if configuredCert != "" {
		certFile = configuredCert
	}

	if configuredKey != "" {
		keyFile = configuredKey
	}

	if fileExists(certFile) && fileExists(keyFile) {
		fingerprint, err := auth.FingerprintOfFile(certFile)
		if err != nil {
			return "", "", err
		}

		fmt.Println("Serving the certificate", certFile)
		fmt.Println("Its fingerprint is", fingerprint)

		return certFile, keyFile, nil
	}

	cert, err := auth.GenerateCertificate(localHosts())
	if err != nil {
		return "", "", err
	}

	if err := auth.WriteCertificate(certFile, keyFile, cert); err != nil {
		return "", "", err
	}

	fmt.Println("Generated a certificate in", certFile)
	fmt.Println("Its fingerprint is", cert.Fingerprint)
	fmt.Println("Give that fingerprint to the other device with --cert-fingerprint.")

	return certFile, keyFile, nil
}

// localHosts returns the names the certificate is valid for: the addresses
// of this machine, so that the certificate says something true.
func localHosts() []string {
	hosts := []string{"localhost", "cthrone"}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return hosts
	}

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		if ip := ipnet.IP; ip.To4() != nil {
			hosts = append(hosts, ip.String())
		}
	}

	return hosts
}

// fileExists reports whether the path is there.
func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// clientTLSConfig returns the configuration the client uses to check the
// certificate of the host.
func clientTLSConfig(tlsConf TLS) (*tls.Config, error) {
	if !tlsConf.Enabled {
		return nil, nil
	}

	if tlsConf.Fingerprint == "" && !tlsConf.Insecure {
		return nil, errors.New("--tls needs --cert-fingerprint to check the certificate, or --insecure to skip the check")
	}

	return auth.ClientTLSConfig(tlsConf.Fingerprint, tlsConf.Insecure)
}
