package websockethandler

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fiwon123/cthrone/internal/session"
)

// Stopping a host hands the port back to the machine: the next host on the
// same port binds it. The GUI's Stop button, the window closing and a start
// cancelled mid-flight all rest on this, and a port that stays bound is the
// "address already in use" a second Host reports.
func TestStopReleasesThePort(t *testing.T) {
	// Ask the kernel for a free port, then give it back: the point is to bind
	// the same number again, so the test cannot use port 0 twice.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		server, err := OpenServer(port, "127.0.0.1", "", "", "", func(session.Session) {})
		if err != nil {
			t.Fatalf("attempt %d: opening on the port a previous stop released: %v", attempt, err)
		}

		if err := server.Stop(); err != nil {
			t.Fatalf("attempt %d: stop: %v", attempt, err)
		}

		// Stop twice is what the GUI does when the button and the window close
		// both fire; it must be a no-op, not a second close.
		if err := server.Stop(); err != nil {
			t.Fatalf("attempt %d: second stop: %v", attempt, err)
		}
	}
}

// A zero Server is what a test stubs to avoid binding a port; stopping it must
// not panic on the fields OpenServer never filled in.
func TestZeroServerStop(t *testing.T) {
	var s Server

	if err := s.Stop(); err != nil {
		t.Errorf("Stop on a zero server = %v, want nil", err)
	}

	if err := s.Wait(); err != nil {
		t.Errorf("Wait on a zero server = %v, want nil", err)
	}
}

// A client that does not know the secret is refused before the session
// starts. The check happens on the request, not after the upgrade: a
// refused client must not get a chat session.
func TestAllowedRefusesAMissingSecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if allowed(recorder, request, "s3cret") {
		t.Fatal("expected a client without the secret to be refused")
	}

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", recorder.Code)
	}
}

// A client that knows the secret is let in.
func TestAllowedAcceptsTheRightSecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("X-Cthrone-Token", "s3cret")

	if !allowed(recorder, request, "s3cret") {
		t.Errorf("expected the client to be let in, got %d", recorder.Code)
	}
}

// A client with the wrong secret is refused. A secret that is a prefix of
// the right one is not the right one.
func TestAllowedRefusesTheWrongSecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("X-Cthrone-Token", "s3cre")

	if allowed(recorder, request, "s3cret") {
		t.Fatal("expected a client with the wrong secret to be refused")
	}
}

// A host that pairs by device key has no secret to check, so the request is
// let through to the handshake, which is the layer that authenticates it.
func TestAllowedLetsTheHandshakeDecideWithoutASecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if !allowed(recorder, request, "") {
		t.Fatalf("a device-key host refused the connection before the handshake, with %d", recorder.Code)
	}
}

// The bind address decides where the server listens. An empty one means
// every interface, so another device on the network can reach it.
func TestListenAddress(t *testing.T) {
	cases := []struct {
		name        string
		bindAddress string
		port        int
		want        string
	}{
		{
			name:        "no bind address means every interface",
			bindAddress: "",
			port:        8080,
			want:        ":8080",
		},
		{
			name:        "a bind address is used",
			bindAddress: "127.0.0.1",
			port:        8080,
			want:        "127.0.0.1:8080",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listenAddress(tc.bindAddress, tc.port); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
