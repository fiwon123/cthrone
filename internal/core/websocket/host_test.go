package websocketcore

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	websockethandler "github.com/fiwon123/cthrone/internal/handlers/websocket"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// stubServer replaces the real server, so a test can look at what OpenServer
// resolved without binding a port.
func stubServer(t *testing.T, capture func(token string)) {
	t.Helper()

	original := server

	t.Cleanup(func() { server = original })

	server = func(port int, bindAddress, token, certFile, keyFile string, onSession func(session.Session)) (*websockethandler.Server, error) {
		if capture != nil {
			capture(token)
		}

		return &websockethandler.Server{}, nil
	}
}

// A host passes the secret it resolved to the server: the handler checks it
// on every client, and a secret the handler never saw would refuse everyone.
func TestOpenServerPassesTheSecretToTheHandler(t *testing.T) {
	var gotToken string

	stubServer(t, func(token string) { gotToken = token })

	secret := "from-env-secret-01"
	t.Setenv(auth.EnvToken, secret)

	handle, err := OpenServer(app.New(8080), pairing.Options{}, auth.TLS{}, func(session.Session) {})
	if err != nil {
		t.Fatal(err)
	}

	if gotToken != secret {
		t.Errorf("expected the secret to be passed on, got %q", gotToken)
	}

	if handle.Token != secret {
		t.Errorf("expected the handle to report the secret, got %q", handle.Token)
	}

	if handle.DeviceFingerprint != "" {
		t.Errorf("a secret host reports a device fingerprint %q", handle.DeviceFingerprint)
	}
}

// A host with no secret anywhere pairs by device key: it generates nothing for
// the other device to copy, and it reports its own fingerprint so a screen can
// show what the other device will compare.
func TestOpenServerPairsByDeviceKeyWhenThereIsNoSecret(t *testing.T) {
	var gotToken string

	stubServer(t, func(token string) { gotToken = token })

	home := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	handle, err := OpenServer(app.New(8080), pairing.Options{}, auth.TLS{}, func(session.Session) {})
	if err != nil {
		t.Fatal(err)
	}

	if gotToken != "" {
		t.Errorf("a device-key host passed the handler a secret: %q", gotToken)
	}

	if handle.Token != "" {
		t.Errorf("a device-key host reported a secret: %q", handle.Token)
	}

	if handle.DeviceFingerprint == "" {
		t.Fatal("the handle does not report the host's own fingerprint")
	}

	path := filepath.Join(home, ".config", "cthrone", "identity.key")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the device key was not written: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the device key is %o, want 600", mode)
	}
}

// The reported fingerprint is the public half of the stored seed, and a second
// host on the same machine is the same device.
func TestDeviceKeyFingerprintIsStable(t *testing.T) {
	stubServer(t, nil)

	home := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	first, err := OpenServer(app.New(8080), pairing.Options{}, auth.TLS{}, func(session.Session) {})
	if err != nil {
		t.Fatal(err)
	}

	second, err := OpenServer(app.New(8080), pairing.Options{}, auth.TLS{}, func(session.Session) {})
	if err != nil {
		t.Fatal(err)
	}

	if first.DeviceFingerprint != second.DeviceFingerprint {
		t.Fatalf("a second host made a new identity: %q then %q", first.DeviceFingerprint, second.DeviceFingerprint)
	}

	stored, err := os.ReadFile(filepath.Join(home, ".config", "cthrone", "identity.key"))
	if err != nil {
		t.Fatal(err)
	}

	seed, err := hex.DecodeString(strings.TrimSpace(string(stored)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("the key file is not a hex seed: %v", err)
	}

	key := ed25519.NewKeyFromSeed(seed)
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok || discovery.Fingerprint(pub) != first.DeviceFingerprint {
		t.Fatal("the reported fingerprint is not the stored key's")
	}
}

// The caller is handed a session only once a peer has proved its identity. A
// connection that never authenticates therefore cannot take over the terminal
// or open a chat: the wrapper is installed here, so a caller cannot forget it,
// and the session it receives is the sealed one.
func TestOpenServerWithholdsTheSessionUntilAPeerAuthenticates(t *testing.T) {
	original := server
	defer func() { server = original }()

	captured := make(chan session.Session, 1)

	server = func(port int, bindAddress, token, certFile, keyFile string, onSession func(session.Session)) (*websockethandler.Server, error) {
		stub := newStub()

		// What the handler does for every client: hand the raw session over.
		onSession(stub)

		if len(stub.outgoing()) == 0 {
			t.Error("the wrapper sent no handshake")
		}

		return &websockethandler.Server{}, nil
	}

	secret := strings.Repeat("s", 32)
	t.Setenv(auth.EnvToken, secret)

	if _, err := OpenServer(app.New(8080), pairing.Options{FlagToken: secret}, auth.TLS{}, func(s session.Session) { captured <- s }); err != nil {
		t.Fatal(err)
	}

	// The stub never completed a handshake, so there is no peer to hand over.
	select {
	case s := <-captured:
		t.Fatalf("the caller received %T before a peer authenticated", s)
	case <-time.After(50 * time.Millisecond):
	}
}

// A secret too short to seal with is refused before the server starts. Waiting
// for the first client would fail inside the per-connection wrapper, where the
// error reaches nobody, and every client would be dropped without a word.
func TestOpenServerRefusesAShortSecret(t *testing.T) {
	served := false
	stubServer(t, func(string) { served = true })

	t.Setenv(auth.EnvToken, "")

	if _, err := OpenServer(app.New(8080), pairing.Options{FlagToken: "short"}, auth.TLS{}, func(session.Session) {}); err == nil {
		t.Fatal("a host accepted a secret too short to seal with")
	}

	if served {
		t.Error("the server started with a secret the handshake cannot use")
	}
}

// A device key that cannot sign is refused before the host serves anything: a
// corrupt identity is not silently replaced with a new one.
func TestOpenServerRefusesABrokenDeviceKey(t *testing.T) {
	stubServer(t, nil)

	home := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	dir := filepath.Join(home, ".config", "cthrone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "identity.key"), []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenServer(app.New(8080), pairing.Options{}, auth.TLS{}, func(session.Session) {}); err == nil {
		t.Fatal("a corrupt device key was replaced with a new identity")
	}
}

// Ctrl-C ends a host, and the port goes back to the machine: a host that
// returned while its listener stayed bound is the "address already in use" the
// next Host reports. The stop is driven through the loop's quit flag, because a
// test cannot press Ctrl-C on a tty.
func TestHostReleasesThePortWhenTheUserQuits(t *testing.T) {
	home := t.TempDir()
	t.Setenv(auth.EnvToken, "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// A fixed port, so the test can bind the same number after the host ends.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	data := app.New(port)
	data.Host = "127.0.0.1"
	data.Name = "host"

	original := runRoom
	t.Cleanup(func() { runRoom = original })

	// The console holds the host open until a device has joined the room, then
	// reports the user quit — the two halves of Ctrl-C, in the order a tty
	// produces them. OnJoin replays a device that joined before the hook was
	// installed, so the wait cannot miss one.
	entered := make(chan struct{}, 1)

	quit := make(chan struct{})
	runRoom = func(hub *session.Hub) bool {
		hub.OnJoin(func(session.Session) {
			select {
			case entered <- struct{}{}:
			default:
			}
		})

		<-quit

		return true
	}

	done := make(chan error, 1)
	go func() {
		done <- Host(data, pairing.Options{
			KeyFile:  filepath.Join(home, "identity.key"),
			PinsFile: filepath.Join(home, "peers.json"),
		}, auth.TLS{})
	}()

	address := fmt.Sprintf("127.0.0.1:%d", port)
	clientData := app.New(0)
	clientData.Name = "client"

	// The dial is what wakes the loop. The retry covers the bind, not a
	// failure: the host is listening just after Host's own listen returns. The
	// client stays open until the loop has it — closing it first would end the
	// handshake before the host ever saw the peer.
	var (
		client    session.Session
		clientErr error
	)

	for attempt := 0; attempt < 100; attempt++ {
		client, clientErr = Dial([]string{address}, clientData, deviceOptions(t.TempDir()), auth.TLS{})
		if clientErr != nil {
			time.Sleep(20 * time.Millisecond)

			continue
		}

		break
	}

	if clientErr != nil {
		close(quit)
		t.Fatalf("no peer ever reached the host, so the quit could not be exercised: %v", clientErr)
	}

	defer client.Close("done")

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(quit)
		t.Fatal("the host never handed the authenticated session to the loop")
	}

	close(quit)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Host returned %v, want nil after a user quit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Host did not return after the user quit the chat loop")
	}

	// The port is back: the next host binds the same number.
	again, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("binding the port Host released: %v", err)
	}

	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}
