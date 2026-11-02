package websocketcore

import (
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/session"
)

// A host's Stop reaches every client, over a real socket: the client's close
// handler runs, and a send afterwards fails instead of disappearing into a
// connection nobody reads.
//
// This is the defect #385 reported. `Server.Stop` closed the listener and the
// HTTP server, and `http.Server.Close` does not close hijacked connections —
// which is what a websocket is — so the client could keep typing into a live
// socket while the host had stopped serving.
func TestStopDisconnectsEveryClient(t *testing.T) {
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

	client, err := Dial([]string{handle.Addr()}, clientData, deviceOptions(clientDir), auth.TLS{})
	if err != nil {
		t.Fatalf("dialling the host: %v", err)
	}
	defer client.Close("done")

	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("the host never let the client in")
	}

	closed := make(chan string, 1)
	client.OnClose(func(reason string) { closed <- reason })

	if err := handle.Stop(); err != nil {
		t.Fatalf("stopping the host: %v", err)
	}

	select {
	case reason := <-closed:
		if reason == "" {
			t.Error("the client was closed with an empty reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the client was not told the host stopped: it is still holding a live socket")
	}

	// And a send now is refused rather than written into the closed session.
	if err := client.Send([]byte("still there?")); err == nil {
		t.Error("a client could still send after the host stopped")
	}
}
