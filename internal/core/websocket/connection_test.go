package websocketcore

import (
	"errors"
	"testing"

	"github.com/fiwon123/cthrone/internal/data/app"
)

// The address of the host comes from the argument. There is no default for
// it — the command used to panic when the argument was missing, and a
// stack trace is not a usage message.
func TestConnectAddress(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no argument means no address",
			args: nil,
			want: "",
		},
		{
			name: "the argument is the address",
			args: []string{"192.168.1.5"},
			want: "192.168.1.5",
		},
		{
			name: "an address with a port is kept as it is",
			args: []string{"192.168.1.5:9000"},
			want: "192.168.1.5:9000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectAddress(tc.args); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// A missing address is an error the command can report, not a panic.
func TestConnectWithoutAnAddressFails(t *testing.T) {
	err := Connect(nil, app.New(8080))
	if err == nil {
		t.Fatal("expected the missing address to be an error")
	}

	if err.Error() != "the address of the host to connect to is required" {
		t.Errorf("expected the error to name what is missing, got %q", err.Error())
	}
}

// The error of the connection comes back out. It used to be dropped, so a
// host that was not running looked like a command that did nothing.
func TestConnectReturnsTheConnectionError(t *testing.T) {
	original := connect
	defer func() { connect = original }()

	connect = func(address string, port int) error {
		return errors.New("no host in this test")
	}

	err := Connect([]string{"192.168.1.5"}, app.New(8080))
	if err == nil {
		t.Fatal("expected the connection to fail")
	}

	if err.Error() != "no host in this test" {
		t.Errorf("expected the connection error, got %q", err.Error())
	}
}
