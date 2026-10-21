package websockethandler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gorilla/websocket"
)

// An address that already carries a port is used as it is: appending the
// port flag to it makes a URL that is not one.
func TestBuildURL(t *testing.T) {
	cases := []struct {
		name    string
		address string
		port    int
		want    string
	}{
		{
			name:    "an address without a port gets the port",
			address: "192.168.1.5",
			port:    8080,
			want:    "ws://192.168.1.5:8080/ws",
		},
		{
			name:    "an address with a port is used as it is",
			address: "192.168.1.5:9000",
			port:    8080,
			want:    "ws://192.168.1.5:9000/ws",
		},
		{
			name:    "a host name works too",
			address: "localhost",
			port:    8080,
			want:    "ws://localhost:8080/ws",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildURL(tc.address, tc.port); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// A connection that fails is reported. The error used to be dropped, so a
// host that was not running looked like a command that did nothing.
func TestConnectReturnsTheDialError(t *testing.T) {
	original := dial
	defer func() { dial = original }()

	dial = func(url string) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("no host in this test")
	}

	err := Connect("192.168.1.5", 8080)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	if err.Error() != "no host in this test" {
		t.Errorf("expected the dial error, got %q", err.Error())
	}
}
