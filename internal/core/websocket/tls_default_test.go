package websocketcore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
)

// A fresh host and client connect over wss:// with no flag and nothing copied
// by hand: the client records the host's certificate on first contact, and the
// message crosses sealed. This is the acceptance case for TLS being the
// default, over a real socket rather than a fake handshake.
func TestTLSIsTheDefaultOverARealSocket(t *testing.T) {
	hostDir, clientDir := t.TempDir(), t.TempDir()

	t.Setenv(auth.EnvToken, "")

	hostData := app.New(0)
	hostData.Host = "127.0.0.1"
	hostData.Name = "host"

	clientData := app.New(0)
	clientData.Host = "127.0.0.1"
	clientData.Name = "client"

	hostTLS := auth.TLS{
		Enabled:  true,
		CertFile: filepath.Join(hostDir, "cert.pem"),
		KeyFile:  filepath.Join(hostDir, "key.pem"),
	}

	joined := make(chan session.Session, 1)

	handle, err := OpenServer(hostData, deviceOptions(hostDir), hostTLS, func(s session.Session) {
		joined <- s
	})
	if err != nil {
		t.Fatalf("starting the host: %v", err)
	}
	defer func() { _ = handle.Stop() }()

	if handle.Fingerprint == "" {
		t.Fatal("a host serving TLS reported no certificate fingerprint")
	}

	client, err := Dial([]string{handle.Addr()}, clientData, deviceOptions(clientDir), auth.TLS{Enabled: true})
	if err != nil {
		t.Fatalf("dialling the host: %v", err)
	}
	defer client.Close("done")

	var host session.Session

	select {
	case host = <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("the host never let the client in")
	}

	// The certificate was recorded under the address that was dialed, and it
	// is the certificate the host reported — nothing was typed between them.
	store, err := discovery.NewPinStore(filepath.Join(clientDir, "peers.json"))
	if err != nil {
		t.Fatalf("reading the client's pins: %v", err)
	}

	certs := store.Certificates()
	if len(certs) != 1 {
		t.Fatalf("the client pinned %d certificates, want 1", len(certs))
	}

	if certs[0].Address != strings.ToLower(handle.Addr()) {
		t.Errorf("pinned under %q, want the dialed address %q", certs[0].Address, strings.ToLower(handle.Addr()))
	}

	if certs[0].Fingerprint != handle.Fingerprint {
		t.Errorf("pinned %s, want the host's %s", certs[0].Fingerprint, handle.Fingerprint)
	}

	received := make(chan session.Message, 1)
	host.OnMessage(func(m session.Message) { received <- m })

	sendOnceTheHandshakeIsDone(t, client, []byte("hello over wss"))

	select {
	case m := <-received:
		if string(m.Payload) != "hello over wss" {
			t.Errorf("received %q, want %q", m.Payload, "hello over wss")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the message never arrived")
	}
}

// sendOnceTheHandshakeIsDone sends payload, waiting out the gap between the
// two halves of the handshake. The host admits the session when the client's
// hello arrives; the client is ready when it has read the host's answer, which
// arrives a few milliseconds later. Nothing is wrong in that window — Send
// refuses rather than sending in the clear, which is the point — so a caller
// that is not the terminal loop waits instead of reporting a refusal.
func sendOnceTheHandshakeIsDone(t *testing.T, s session.Session, payload []byte) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		err := s.Send(payload)
		if err == nil {
			return
		}

		// A closed session is a refused handshake, not a slow one: only
		// "not yet" is worth waiting for.
		if !errors.Is(err, e2e.ErrHandshake) || time.Now().After(deadline) {
			t.Fatalf("sending: %v", err)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// A client that still speaks plaintext — a v0.6.0 one, or one started with
// --no-tls — is refused by the default host, and the refusal names the way
// out rather than leaving a bare handshake error.
func TestAPlaintextClientIsRefusedByTheDefaultHost(t *testing.T) {
	hostDir, clientDir := t.TempDir(), t.TempDir()

	t.Setenv(auth.EnvToken, "")

	hostData := app.New(0)
	hostData.Host = "127.0.0.1"
	hostData.Name = "host"

	hostTLS := auth.TLS{
		Enabled:  true,
		CertFile: filepath.Join(hostDir, "cert.pem"),
		KeyFile:  filepath.Join(hostDir, "key.pem"),
	}

	handle, err := OpenServer(hostData, deviceOptions(hostDir), hostTLS, func(session.Session) {})
	if err != nil {
		t.Fatalf("starting the host: %v", err)
	}
	defer func() { _ = handle.Stop() }()

	clientData := app.New(0)
	clientData.Name = "client"

	_, err = Dial([]string{handle.Addr()}, clientData, deviceOptions(clientDir), auth.TLS{})
	if err == nil {
		t.Fatal("a plaintext client reached a host serving TLS")
	}

	if !strings.Contains(err.Error(), "--no-tls") {
		t.Errorf("the refusal does not name the way out: %v", err)
	}
}
