package websocketcore

import (
	"net"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
)

func TestSubnetFromIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want string
	}{
		{"192.168", "192.168.1.42", "192.168.1."},
		{"10", "10.0.5.7", "10.0.5."},
		{"172.16", "172.16.0.10", "172.16.0."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subnetFromIP(tc.ip); got != tc.want {
				t.Errorf("subnetFromIP(%q) = %q, want %q", tc.ip, got, tc.want)
			}
		})
	}
}

func TestSelectLocalIPPrefersRFC1918(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
		&net.IPNet{IP: net.ParseIP("8.8.8.8")},
		&net.IPNet{IP: net.ParseIP("10.0.5.7")},
	}
	ip, ok := selectLocalIP(addrs)
	if !ok {
		t.Fatal("expected to find an IP")
	}
	if ip != "10.0.5.7" {
		t.Errorf("expected 10.0.5.7, got %q", ip)
	}
}

func TestSelectLocalIPFallsBackToNonLoopback(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
		&net.IPNet{IP: net.ParseIP("8.8.8.8")},
	}
	ip, ok := selectLocalIP(addrs)
	if !ok {
		t.Fatal("expected to find an IP")
	}
	if ip != "8.8.8.8" {
		t.Errorf("expected 8.8.8.8, got %q", ip)
	}
}

func TestSelectLocalIPLoopbackOnly(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
	}
	_, ok := selectLocalIP(addrs)
	if ok {
		t.Fatal("expected no IP to be found")
	}
}

func TestSelectLocalIPPicks172Range(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("172.16.0.5")},
	}
	ip, ok := selectLocalIP(addrs)
	if !ok {
		t.Fatal("expected to find an IP")
	}
	if ip != "172.16.0.5" {
		t.Errorf("expected 172.16.0.5, got %q", ip)
	}
}

func TestScanFindsHostsOn10Network(t *testing.T) {
	origLocal := localIP
	origDial := dialScan
	origAddrs := localAddrs
	defer func() {
		localIP = origLocal
		dialScan = origDial
		localAddrs = origAddrs
	}()

	localIP = func() (string, error) { return "10.0.5.7", nil }
	localAddrs = func() []string { return []string{"10.0.5.7"} }
	dialScan = func(addr string, timeout time.Duration) error {
		if addr == "10.0.5.10:8080" {
			return nil
		}
		return net.ErrClosed
	}

	app := app.New(8080)
	found := Scan(app)
	if len(found) != 1 {
		t.Fatalf("expected 1 host, got %v", found)
	}
	if found[0] != "10.0.5.10" {
		t.Errorf("expected 10.0.5.10, got %q", found[0])
	}
}

// A host answers on its own address, so a scan that kept every answer lists
// this device as a peer to connect to. Every address the machine holds is
// dropped, not just the one the subnet walk starts from.
func TestScanSkipsThisDevice(t *testing.T) {
	origLocal := localIP
	origDial := dialScan
	origAddrs := localAddrs
	defer func() {
		localIP = origLocal
		dialScan = origDial
		localAddrs = origAddrs
	}()

	localIP = func() (string, error) { return "10.0.5.7", nil }
	localAddrs = func() []string { return []string{"10.0.5.7", "192.168.1.5"} }
	// Every address answers: the filter, not the dial, is what must drop the
	// device itself.
	dialScan = func(string, time.Duration) error { return nil }

	found := Scan(app.New(8080))

	for _, ip := range found {
		if ip == "10.0.5.7" {
			t.Fatalf("scan reported this device's own address %q", ip)
		}
	}
	if len(found) != 253 {
		t.Fatalf("expected 253 addresses (.1-.254 without this device), got %d", len(found))
	}
}
