package websocketcore

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/session"
)

// host is the function that serves the chat endpoint. It is a variable so a
// test can replace it: serving means listening on a port, and a test cannot
// do that.
var host = websockethandler.Host

// HostHandle is a running host. It carries the two values the other device
// needs to pair — the shared secret and, with TLS, the certificate
// fingerprint — because the caller may have no terminal to print them to.
type HostHandle struct {
	server *websockethandler.Server

	// Token is the secret the host requires. It is empty when the host
	// allows anonymous clients.
	Token string

	// Fingerprint is the certificate the other device must pin. It is empty
	// when TLS is off.
	Fingerprint string

	// Announcer is optional; stop it when closing.
	announcer interface{ Stop() }
}

// Addr is the address the host is listening on.
func (h *HostHandle) Addr() string { return h.server.Addr() }

// Stop closes the listener and drops the connections it holds.
func (h *HostHandle) Stop() error {
	if h.announcer != nil {
		h.announcer.Stop()
	}
	return h.server.Stop()
}

// Host open connection
func Host(app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS) error {
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

	if err := host(app.Port, app.Host, token, app.AllowAnonymous, certFile, keyFile); err != nil {
		return err
	}
	name := app.Name
	if name == "" {
		name = "cthrone"
	}
	ann := discovery.NewAnnouncer(name, app.Port)
	ann.Start(2 * time.Second)
	// block to keep announcer alive
	select {}
}

// OpenServer starts a host and returns a handle for a caller that is not the
// terminal — the GUI and the mobile app. It binds the listener before it
// returns, so Addr reports the port that is actually open, and the caller can
// Stop it without ending the process.
//
// The handle also reports the shared secret and the certificate fingerprint:
// the terminal prints them, and a GUI has nowhere to print them to.
func OpenServer(app *app.Data, flagToken, tokenFile string, tlsConf auth.TLS, onSession func(session.Session)) (*HostHandle, error) {
	token := ""
	if !app.AllowAnonymous {
		var err error

		token, err = hostToken(tokenConfig{flagValue: flagToken, filePath: tokenFile})
		if err != nil {
			return nil, err
		}
	}

	certFile, keyFile := "", ""
	fingerprint := ""
	if tlsConf.Enabled {
		var err error

		certFile, keyFile, err = hostCertificate(tlsConf.CertFile, tlsConf.KeyFile)
		if err != nil {
			return nil, err
		}

		// The certificate is on disk at this point, so its fingerprint can be
		// read back without generating anything twice.
		if fingerprint, err = auth.FingerprintOfFile(certFile); err != nil {
			return nil, err
		}
	}

	server, err := websockethandler.OpenServer(app.Port, app.Host, token, app.AllowAnonymous, certFile, keyFile, onSession)
	if err != nil {
		return nil, err
	}

	name := app.Name
	if name == "" {
		name = "cthrone"
	}
	ann := discovery.NewAnnouncer(name, app.Port)
	ann.Start(2 * time.Second)

	return &HostHandle{server: server, Token: token, Fingerprint: fingerprint, announcer: ann}, nil
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
func clientTLSConfig(tlsConf auth.TLS) (*tls.Config, error) {
	return auth.TLSConfig(tlsConf)
}
