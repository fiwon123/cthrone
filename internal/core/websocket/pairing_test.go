package websocketcore

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// deviceOptions is one simulated device's files: a token file that does not
// exist (so it pairs by key), its own identity and its own pin store.
func deviceOptions(dir string) pairing.Options {
	return pairing.Options{
		TokenFile: filepath.Join(dir, "token"),
		KeyFile:   filepath.Join(dir, "identity.key"),
		PinsFile:  filepath.Join(dir, "peers.json"),
	}
}

// fingerprintOf reads the identity a device wrote and returns the half its peer
// sees.
func fingerprintOf(t *testing.T, dir string) string {
	t.Helper()

	stored, err := os.ReadFile(filepath.Join(dir, "identity.key"))
	if err != nil {
		t.Fatalf("reading the device key: %v", err)
	}

	seed, err := hex.DecodeString(strings.TrimSpace(string(stored)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("the device key is not a hex seed: %v", err)
	}

	pub, ok := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("the device key has no public half")
	}

	return discovery.Fingerprint(pub)
}

// assertPinned checks that dir's pin store holds name under fingerprint.
func assertPinned(t *testing.T, dir, name, fingerprint string) {
	t.Helper()

	store, err := discovery.NewPinStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("reading the pins: %v", err)
	}

	for _, p := range store.List() {
		if p.Name != name {
			continue
		}

		if p.Fingerprint != fingerprint {
			t.Errorf("%s pinned %q as %s, want %s", dir, name, p.Fingerprint, fingerprint)
		}

		return
	}

	t.Errorf("%s never pinned %q", dir, name)
}

// Two devices that share no value pair over a real socket: the host generates
// its key instead of a secret, each side pins the other's key on first contact,
// and the message crosses sealed.
func TestDeviceKeyPairingOverARealSocket(t *testing.T) {
	hostDir, clientDir := t.TempDir(), t.TempDir()

	t.Setenv(auth.EnvToken, "")

	hostData := app.New(0)
	hostData.Host = "127.0.0.1"
	hostData.Name = "host"

	clientData := app.New(0)
	clientData.Host = "127.0.0.1"
	clientData.Name = "client"

	joined := make(chan session.Session, 1)

	handle, err := OpenServer(hostData, deviceOptions(hostDir), auth.TLS{}, func(s session.Session) {
		joined <- s
	})
	if err != nil {
		t.Fatalf("starting the host: %v", err)
	}
	defer func() { _ = handle.Stop() }()

	if handle.Token != "" {
		t.Errorf("a device-key host reported a shared secret %q", handle.Token)
	}

	if handle.DeviceFingerprint != fingerprintOf(t, hostDir) {
		t.Errorf("the host reported %q, want its own key's fingerprint", handle.DeviceFingerprint)
	}

	client, err := Dial([]string{handle.Addr()}, clientData, deviceOptions(clientDir), auth.TLS{})
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

	// What the host's consumer receives is the sealed session, and it arrives
	// only after the handshake: an unauthenticated client is never handed over.
	if _, ok := host.(*e2e.Conn); !ok {
		t.Fatalf("the host handed over %T, want the sealed session", host)
	}

	sealed, ok := client.(*e2e.Conn)
	if !ok {
		t.Fatalf("the client is %T, want the sealed session", client)
	}

	paired := make(chan e2e.Peer, 1)
	sealed.OnPeer(func(p e2e.Peer) { paired <- p })

	select {
	case p := <-paired:
		if !p.First {
			t.Error("the first contact was not reported as such")
		}

		if want := fingerprintOf(t, hostDir); p.Fingerprint != want {
			t.Errorf("the client pinned the host as %q, want %q", p.Fingerprint, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client never verified the host")
	}

	received := make(chan session.Message, 1)
	host.OnMessage(func(m session.Message) { received <- m })

	if err := client.Send([]byte("hello over the wire")); err != nil {
		t.Fatalf("sending: %v", err)
	}

	select {
	case m := <-received:
		if string(m.Payload) != "hello over the wire" {
			t.Errorf("the host received %q", m.Payload)
		}

		if m.From != "client" {
			t.Errorf("the message came from %q, want the device that signed the hello", m.From)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host never received the message")
	}

	// Both sides wrote the other's key to their own pin store, and nowhere did
	// a secret change hands.
	assertPinned(t, hostDir, "client", fingerprintOf(t, clientDir))
	assertPinned(t, clientDir, "host", fingerprintOf(t, hostDir))
}
