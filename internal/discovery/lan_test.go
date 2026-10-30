package discovery

import (
	"errors"
	"net"
	"testing"
)

// stubConn is the probe socket a test hands back instead of a real one: only
// LocalAddr and Close are reached, so nothing else has to work.
type stubConn struct {
	net.Conn

	local net.Addr
}

func (c stubConn) LocalAddr() net.Addr { return c.local }

func (c stubConn) Close() error { return nil }

func TestDefaultRouteIP(t *testing.T) {
	cases := []struct {
		name   string
		dial   func() (net.Conn, error)
		want   string
		wantOK bool
	}{
		{
			name:   "the route's source address",
			dial:   func() (net.Conn, error) { return stubConn{local: &net.UDPAddr{IP: net.ParseIP("192.168.1.5")}}, nil },
			want:   "192.168.1.5",
			wantOK: true,
		},
		{
			name: "an IPv4-mapped address is IPv4",
			dial: func() (net.Conn, error) {
				return stubConn{local: &net.UDPAddr{IP: net.ParseIP("::ffff:10.0.5.7")}}, nil
			},
			want:   "10.0.5.7",
			wantOK: true,
		},
		{
			name: "no default route",
			dial: func() (net.Conn, error) { return nil, errors.New("network is unreachable") },
		},
		{
			name:   "a socket without an address",
			dial:   func() (net.Conn, error) { return stubConn{}, nil },
			wantOK: false,
		},
		{
			name:   "a socket on the loopback",
			dial:   func() (net.Conn, error) { return stubConn{local: &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}}, nil },
			wantOK: false,
		},
		{
			name:   "a socket bound to every address",
			dial:   func() (net.Conn, error) { return stubConn{local: &net.UDPAddr{IP: net.IPv4zero}}, nil },
			wantOK: false,
		},
		{
			name: "an address that is not a UDP address",
			dial: func() (net.Conn, error) {
				return stubConn{local: &net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 8080}}, nil
			},
			wantOK: false,
		},
	}

	orig := dialRoute
	defer func() { dialRoute = orig }()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialRoute = tc.dial

			got, ok := DefaultRouteIP()

			if ok != tc.wantOK {
				t.Fatalf("DefaultRouteIP() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("DefaultRouteIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdvertisedAddrs(t *testing.T) {
	cases := []struct {
		name      string
		addrs     []string
		preferred string
		want      []string
	}{
		{
			name:      "a docker bridge and a VPN are not advertised",
			addrs:     []string{"172.17.0.1", "10.8.0.6", "192.168.1.5"},
			preferred: "192.168.1.5",
			want:      []string{"192.168.1.5"},
		},
		{
			name:      "a second address on the same LAN follows the route's one",
			addrs:     []string{"10.0.5.9", "10.0.5.7"},
			preferred: "10.0.5.7",
			want:      []string{"10.0.5.7", "10.0.5.9"},
		},
		{
			name:      "another /24 of the same private range is not a way in",
			addrs:     []string{"192.168.1.5", "192.168.2.5"},
			preferred: "192.168.1.5",
			want:      []string{"192.168.1.5"},
		},
		{
			name:      "the route address leads even when the list does not carry it",
			addrs:     []string{"192.168.1.5", "192.168.1.6"},
			preferred: "192.168.1.9",
			want:      []string{"192.168.1.9", "192.168.1.5", "192.168.1.6"},
		},
		{
			name:  "no default route keeps every address in order",
			addrs: []string{"172.17.0.1", "192.168.1.5"},
			want:  []string{"172.17.0.1", "192.168.1.5"},
		},
		{
			name:      "no addresses and no route advertise nothing",
			preferred: "",
			want:      nil,
		},
		{
			name:      "a route without interfaces advertises nothing",
			preferred: "192.168.1.5",
			want:      nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AdvertisedAddrs(tc.addrs, tc.preferred)

			if len(got) != len(tc.want) {
				t.Fatalf("AdvertisedAddrs(%v, %q) = %v, want %v", tc.addrs, tc.preferred, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("AdvertisedAddrs(%v, %q) = %v, want %v", tc.addrs, tc.preferred, got, tc.want)
				}
			}
		})
	}
}

// The returned slice belongs to the caller: writing to it must not edit the
// address list the scanner and the GUI share.
func TestAdvertisedAddrsCopiesTheList(t *testing.T) {
	addrs := []string{"192.168.1.5", "172.17.0.1"}

	got := AdvertisedAddrs(addrs, "")
	got[0] = "10.9.9.9"

	if addrs[0] != "192.168.1.5" {
		t.Fatalf("AdvertisedAddrs returned a slice that aliases its input: %v", addrs)
	}
}
