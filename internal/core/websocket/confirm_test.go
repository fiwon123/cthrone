package websocketcore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
)

// assertNotPinned checks that dir's pin store holds no entry under name.
func assertNotPinned(t *testing.T, dir, name string) {
	t.Helper()

	store, err := discovery.NewPinStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		// No file at all is the strongest form of "nothing was pinned".
		if !os.IsNotExist(err) {
			t.Fatalf("reading the pins: %v", err)
		}

		return
	}

	for _, p := range store.List() {
		if p.Name == name {
			t.Errorf("%s pinned %q, want nothing pinned", dir, name)
		}
	}
}

// firstContactHost starts a websocket host with a confirmation gate and returns
// the channel its consumer arrives on, and the address to dial.
func firstContactHost(t *testing.T, dir string, gate Confirm) (<-chan session.Session, string) {
	t.Helper()

	data := app.New(0)
	data.Host = "127.0.0.1"
	data.Name = "host"

	joined := make(chan session.Session, 1)

	handle, err := OpenServer(data, deviceOptions(dir), auth.TLS{}, func(s session.Session) {
		joined <- s
	}, gate)
	if err != nil {
		t.Fatalf("starting the host: %v", err)
	}

	t.Cleanup(func() { _ = handle.Stop() })

	return joined, handle.Addr()
}

// dialClient connects a device of its own to addr.
func dialClient(t *testing.T, dir, addr string) *e2e.Conn {
	t.Helper()

	data := app.New(0)
	data.Host = "127.0.0.1"
	data.Name = "client"

	client, err := Dial([]string{addr}, data, deviceOptions(dir), auth.TLS{})
	if err != nil {
		t.Fatalf("dialling the host: %v", err)
	}

	t.Cleanup(func() { client.Close("done") })

	sealed, ok := client.(*e2e.Conn)
	if !ok {
		t.Fatalf("the client is %T, want the sealed session", client)
	}

	return sealed
}

// The gate a host is given reaches the handshake: the question carries the
// name, the fingerprint and the code, an accepted device is handed to the
// consumer, and both sides pin each other.
func TestOpenServerAsksBeforePinningAFirstContact(t *testing.T) {
	hostDir, clientDir := t.TempDir(), t.TempDir()

	t.Setenv(auth.EnvToken, "")

	asked := make(chan e2e.FirstContact, 1)

	joined, addr := firstContactHost(t, hostDir, func(c e2e.FirstContact) bool {
		asked <- c

		return true
	})

	client := dialClient(t, clientDir, addr)

	// The client's own view of the pairing: it pins the host once the confirm
	// the host withheld arrives, which is the last step of an accepted contact.
	paired := make(chan e2e.Peer, 1)

	client.OnPeer(func(p e2e.Peer) { paired <- p })

	select {
	case c := <-asked:
		if c.Name != "client" {
			t.Errorf("the host asked about %q, want client", c.Name)
		}

		if c.Code == "" {
			t.Error("the question carried no code")
		}

		if want := fingerprintOf(t, clientDir); c.Fingerprint != want {
			t.Errorf("the question showed %q, want %q", c.Fingerprint, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host never asked about the device")
	}

	select {
	case s := <-joined:
		if _, ok := s.(*e2e.Conn); !ok {
			t.Fatalf("the host handed over %T, want the sealed session", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host never let the accepted client in")
	}

	select {
	case <-paired:
	case <-time.After(5 * time.Second):
		t.Fatal("the client never pinned the host it was accepted by")
	}

	assertPinned(t, hostDir, "client", fingerprintOf(t, clientDir))
	assertPinned(t, clientDir, "host", fingerprintOf(t, hostDir))
}

// A refused device is never handed to the consumer and leaves no pin behind:
// the host's listener stays up for the next one.
func TestOpenServerRefusesARefusedFirstContact(t *testing.T) {
	hostDir, clientDir := t.TempDir(), t.TempDir()

	t.Setenv(auth.EnvToken, "")

	joined, addr := firstContactHost(t, hostDir, func(e2e.FirstContact) bool { return false })

	dialClient(t, clientDir, addr)

	select {
	case s := <-joined:
		t.Fatalf("the host handed over %T to a device it refused", s)
	case <-time.After(500 * time.Millisecond):
	}

	assertNotPinned(t, hostDir, "client")
	assertNotPinned(t, clientDir, "host")
}
