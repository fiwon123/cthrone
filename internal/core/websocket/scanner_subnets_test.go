package websocketcore

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
)

// stubDial replaces the dialer and the local address list, and restores both.
func stubDial(t *testing.T, own []string, dial func(addr string) error) *[]string {
	t.Helper()

	origDial, origAddrs := dialScan, localAddrs

	var mu sync.Mutex
	var dialed []string

	t.Cleanup(func() { dialScan, localAddrs = origDial, origAddrs })

	localAddrs = func() []string { return own }
	dialScan = func(addr string, _ time.Duration) error {
		mu.Lock()
		dialed = append(dialed, addr)
		mu.Unlock()

		return dial(addr)
	}

	return &dialed
}

func TestParseSubnets(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    []string
		wantErr bool
	}{
		{
			name:   "one /24",
			values: []string{"192.168.1.0/24"},
			want:   []string{"192.168.1.0/24"},
		},
		{
			name:   "an address inside a subnet is masked to the network",
			values: []string{"192.168.1.5/24"},
			want:   []string{"192.168.1.0/24"},
		},
		{
			name:   "surrounding space and an empty entry are ignored",
			values: []string{" 10.0.0.0/24 ", ""},
			want:   []string{"10.0.0.0/24"},
		},
		{
			name:   "several segments keep their order",
			values: []string{"192.168.1.0/24", "10.0.0.0/24"},
			want:   []string{"192.168.1.0/24", "10.0.0.0/24"},
		},
		{
			name:    "a bare address is not a CIDR",
			values:  []string{"192.168.1.5"},
			wantErr: true,
		},
		{
			name:    "a nonsense value is refused",
			values:  []string{"not-a-subnet"},
			wantErr: true,
		},
		{
			name:    "an out-of-range octet is refused",
			values:  []string{"300.1.1.0/24"},
			wantErr: true,
		},
		{
			name:    "IPv6 is refused rather than walked",
			values:  []string{"::1/128"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSubnets(tt.values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSubnets(%q) error = %v, wantErr %v", tt.values, err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("ParseSubnets(%q) = %v, want %v", tt.values, got, tt.want)
			}

			for i := range tt.want {
				if got[i].String() != tt.want[i] {
					t.Errorf("prefix %d = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestScanHostsCountsHosts(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantCount int
		wantFirst string
		wantLast  string
	}{
		{"a /24 skips network and broadcast", "192.168.1.0/24", 254, "192.168.1.1", "192.168.1.254"},
		{"a /30 keeps the two usable addresses", "10.0.0.0/30", 2, "10.0.0.1", "10.0.0.2"},
		{"a /31 is both addresses, RFC 3021", "10.0.0.6/31", 2, "10.0.0.6", "10.0.0.7"},
		{"a /32 is the one address", "10.0.0.7/32", 1, "10.0.0.7", "10.0.0.7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hosts, err := scanHosts([]netip.Prefix{netip.MustParsePrefix(tt.prefix)})
			if err != nil {
				t.Fatalf("scanHosts(%s) error = %v", tt.prefix, err)
			}

			if len(hosts) != tt.wantCount {
				t.Fatalf("scanHosts(%s) = %d addresses, want %d", tt.prefix, len(hosts), tt.wantCount)
			}

			if hosts[0] != tt.wantFirst || hosts[len(hosts)-1] != tt.wantLast {
				t.Errorf("scanHosts(%s) spans %s..%s, want %s..%s",
					tt.prefix, hosts[0], hosts[len(hosts)-1], tt.wantFirst, tt.wantLast)
			}
		})
	}
}

// A mistyped /16 is 65,534 dials. The cap has to refuse it before anything is
// allocated or dialled, or the command appears to hang.
func TestScanHostsRefusesOversizedPrefix(t *testing.T) {
	for _, prefix := range []string{"192.168.0.0/16", "0.0.0.0/0", "10.0.0.0/8"} {
		t.Run(prefix, func(t *testing.T) {
			_, err := scanHosts([]netip.Prefix{netip.MustParsePrefix(prefix)})
			if !errors.Is(err, ErrTooManyHosts) {
				t.Fatalf("scanHosts(%s) error = %v, want ErrTooManyHosts", prefix, err)
			}
		})
	}
}

// The cap is on the whole run, not per prefix: twenty /24s are 5,080 addresses
// even though each one alone is under the limit.
func TestScanHostsCapsTheTotalAcrossPrefixes(t *testing.T) {
	prefixes := make([]netip.Prefix, 0, 20)
	for i := 0; i < 20; i++ {
		prefixes = append(prefixes, netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i), 0, 0}), 24))
	}

	if _, err := scanHosts(prefixes); !errors.Is(err, ErrTooManyHosts) {
		t.Fatalf("scanHosts over %d prefixes error = %v, want ErrTooManyHosts", len(prefixes), err)
	}
}

func TestScanSubnetsWalksEveryRequestedSubnet(t *testing.T) {
	dialed := stubDial(t, nil, func(addr string) error {
		if addr == "10.0.0.2:8080" {
			return nil
		}

		return net.ErrClosed
	})

	found, err := ScanSubnets(app.New(8080), []netip.Prefix{
		netip.MustParsePrefix("192.168.1.0/30"),
		netip.MustParsePrefix("10.0.0.0/30"),
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("ScanSubnets error = %v", err)
	}

	if len(*dialed) != 4 {
		t.Fatalf("dialled %v, want the 4 usable addresses of both /30s", *dialed)
	}

	if len(found) != 1 || found[0] != "10.0.0.2" {
		t.Fatalf("found %v, want [10.0.0.2]", found)
	}
}

// This device answers on its own address, so a walk of a segment it is on must
// not report it as a peer — on every subnet, not just the first.
func TestScanSubnetsSkipsOwnAddressesAcrossSubnets(t *testing.T) {
	stubDial(t, []string{"10.0.0.1", "10.0.1.1"}, func(string) error { return nil })

	found, err := ScanSubnets(app.New(8080), []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/30"),
		netip.MustParsePrefix("10.0.1.0/30"),
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("ScanSubnets error = %v", err)
	}

	for _, ip := range found {
		if ip == "10.0.0.1" || ip == "10.0.1.1" {
			t.Fatalf("scan reported this device's own address %q", ip)
		}
	}

	if len(found) != 2 {
		t.Fatalf("found %v, want the two other addresses", found)
	}
}

// The walk is bounded: a /24 must not open 254 sockets at once. The pool is
// the difference between a scan and a small SYN flood.
func TestScanSubnetsBoundsTheDialsInFlight(t *testing.T) {
	var inFlight, peak int64

	stubDial(t, nil, func(string) error {
		now := atomic.AddInt64(&inFlight, 1)

		for {
			old := atomic.LoadInt64(&peak)
			if now <= old || atomic.CompareAndSwapInt64(&peak, old, now) {
				break
			}
		}

		time.Sleep(time.Millisecond)
		atomic.AddInt64(&inFlight, -1)

		return net.ErrClosed
	})

	if _, err := ScanSubnets(app.New(8080), []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}, time.Millisecond); err != nil {
		t.Fatalf("ScanSubnets error = %v", err)
	}

	if peak > scanWorkers {
		t.Fatalf("%d dials were in flight at once, want at most %d", peak, scanWorkers)
	}

	if peak < 2 {
		t.Fatalf("peak concurrency was %d: the walk looks serial", peak)
	}
}

func TestScanSubnetsRefusesOversizedWithoutDialling(t *testing.T) {
	dialed := stubDial(t, nil, func(string) error { return nil })

	_, err := ScanSubnets(app.New(8080), []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")}, time.Millisecond)
	if !errors.Is(err, ErrTooManyHosts) {
		t.Fatalf("ScanSubnets error = %v, want ErrTooManyHosts", err)
	}

	if len(*dialed) != 0 {
		t.Fatalf("dialled %d addresses before refusing the prefix", len(*dialed))
	}
}

func TestLocalSubnets(t *testing.T) {
	orig := interfaceAddrs
	t.Cleanup(func() { interfaceAddrs = orig })

	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("169.254.10.4"), Mask: net.CIDRMask(16, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.0.5"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.0.9"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("10.1.2.3"), Mask: net.CIDRMask(16, 32)},
			&net.IPNet{IP: net.ParseIP("172.16.4.9"), Mask: net.CIDRMask(28, 32)},
		}, nil
	}

	got := LocalSubnets()

	want := []string{"10.1.2.0/24", "172.16.4.0/28", "192.168.0.0/24"}
	if len(got) != len(want) {
		t.Fatalf("LocalSubnets() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("subnet %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// A machine with no usable interface must report nothing rather than panic or
// invent a subnet.
func TestLocalSubnetsWithoutInterfaces(t *testing.T) {
	orig := interfaceAddrs
	t.Cleanup(func() { interfaceAddrs = orig })

	interfaceAddrs = func() ([]net.Addr, error) { return nil, net.ErrClosed }

	if got := LocalSubnets(); len(got) != 0 {
		t.Fatalf("LocalSubnets() = %v, want none", got)
	}
}
