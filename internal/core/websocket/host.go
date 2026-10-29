package websocketcore

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// server is the function that starts the web socket host. It is a variable so
// a test can replace it: serving means listening on a port, and a test cannot
// do that.
var server = websockethandler.OpenServer

// HostHandle is a running host. It carries the two values the other device
// needs to pair — the shared secret, or this device's fingerprint in the
// device-key model, and, with TLS, the certificate fingerprint — because the
// caller may have no terminal to print them to.
type HostHandle struct {
	server *websockethandler.Server

	// Token is the secret the host requires. It is empty when the host is
	// pairing by device key: there is then nothing to copy to the other
	// device, and the end-to-end handshake is what lets a peer in.
	Token string

	// DeviceFingerprint is this host's own device key in the device-key model:
	// what the other device shows when the two meet, for comparison. It is
	// empty when a shared secret is used.
	DeviceFingerprint string

	// Fingerprint is the certificate the other device must pin. It is empty
	// when TLS is off.
	Fingerprint string

	// Announcer is optional; stop it when closing.
	announcer interface{ Stop() }
}

// Addr is the address the host is listening on.
func (h *HostHandle) Addr() string { return h.server.Addr() }

// Wait blocks until the host stops serving.
func (h *HostHandle) Wait() error { return h.server.Wait() }

// Stop closes the listener and drops the connections it holds.
func (h *HostHandle) Stop() error {
	if h.announcer != nil {
		h.announcer.Stop()
	}

	return h.server.Stop()
}

// Host serves the chat endpoint with the terminal as its consumer.
//
// The bind address is empty by default, which means every interface: the tool
// is a chat between two devices on the same network, and a host that only
// listens on the loopback cannot be reached by the other device.
func Host(app *app.Data, opts pairing.Options, tlsConf auth.TLS) error {
	handle, err := OpenServer(app, opts, tlsConf, func(s session.Session) {
		go runTerminal(s)
	})
	if err != nil {
		return err
	}

	log.Println("WebSocket server listening on", handle.Addr(), "-", describeBind(app.Host))

	return handle.Wait()
}

// OpenServer starts a host and returns a handle for a caller that is not the
// terminal — the GUI and the mobile app. It binds the listener before it
// returns, so Addr reports the port that is actually open, and the caller can
// Stop it without ending the process.
//
// Every client that is let in arrives at onSession already sealed: the
// wrapper is installed here, so a caller cannot forget it and cannot opt out.
//
// The handle also reports what the other device needs to pair — the shared
// secret, or this device's fingerprint in the device-key model — and the
// certificate fingerprint: the terminal prints them, and a GUI has nowhere to
// print them to.
func OpenServer(app *app.Data, opts pairing.Options, tlsConf auth.TLS, onSession func(session.Session)) (*HostHandle, error) {
	cred, err := pairing.Load(opts)
	if err != nil {
		return nil, err
	}

	token := cred.Secret()

	certFile, keyFile := "", ""
	fingerprint := ""
	if tlsConf.Enabled {
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

	// hub is filled in once the server exists: admit runs per connection, and
	// the handle it belongs to is only returned below. ready closes at the same
	// moment, and the close is what makes the write to hub visible to an
	// admission that started before it — so a client that connects the instant
	// the listener opens still lands in the hub.
	var (
		hub   *session.Hub
		ready = make(chan struct{})
	)

	// admit hands an authenticated session to the caller and records it in the
	// hub. Only a sealed session ever reaches it, so a broadcast cannot write
	// to the transport in the clear.
	admit := func(sealed session.Session) {
		<-ready

		if hub != nil {
			hub.Register(sealed)
			sealed.OnClose(func(string) { hub.Unregister(sealed.ID()) })
		}

		onSession(sealed)
	}

	srv, err := server(app.Port, app.Host, token, certFile, keyFile, func(raw session.Session) {
		sealed, err := seal(raw, cred, app.Name)
		if err != nil {
			return
		}

		// The consumer starts on the first peer that proves its identity, not
		// on the socket. A visitor that never authenticates therefore cannot
		// take over the terminal — which puts it in raw mode — or open a chat
		// in the app without holding a key this device accepts.
		conn, ok := sealed.(*e2e.Conn)
		if !ok {
			admit(sealed)

			return
		}

		var once sync.Once
		conn.OnPeer(func(e2e.Peer) { once.Do(func() { admit(sealed) }) })
	})
	if err != nil {
		return nil, err
	}

	hub = srv.Hub()
	close(ready)

	name := app.Name
	if name == "" {
		name = "cthrone"
	}
	ann := discovery.NewAnnouncer(name, app.Port)
	ann.Start(2 * time.Second)

	return &HostHandle{
		server:            srv,
		Token:             token,
		DeviceFingerprint: cred.Fingerprint(),
		Fingerprint:       fingerprint,
		announcer:         ann,
	}, nil
}

// describeBind says what the bind address means, so a host status is not just
// a colon and a port.
func describeBind(bindAddress string) string {
	if bindAddress == "" {
		return "every interface — reachable from the local network"
	}
	if bindAddress == "127.0.0.1" || bindAddress == "localhost" {
		return "loopback only — this machine alone"
	}

	return bindAddress + " — a specific interface"
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
