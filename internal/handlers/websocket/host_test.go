package websockethandler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A client that does not know the secret is refused before the session
// starts. The check happens on the request, not after the upgrade: a
// refused client must not get a chat session.
func TestAllowedRefusesAMissingSecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if allowed(recorder, request, "s3cret", false) {
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

	if !allowed(recorder, request, "s3cret", false) {
		t.Errorf("expected the client to be let in, got %d", recorder.Code)
	}
}

// A client with the wrong secret is refused. A secret that is a prefix of
// the right one is not the right one.
func TestAllowedRefusesTheWrongSecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("X-Cthrone-Token", "s3cre")

	if allowed(recorder, request, "s3cret", false) {
		t.Fatal("expected a client with the wrong secret to be refused")
	}
}

// A host that allows anonymous connections lets every client in. It is the
// state the tool was in before the secret existed, and it has to be asked
// for.
func TestAllowedAcceptsAnyClientWhenAnonymous(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if !allowed(recorder, request, "s3cret", true) {
		t.Errorf("expected an anonymous client to be let in, got %d", recorder.Code)
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
