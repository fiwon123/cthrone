package discovery

import (
	"net"
	"testing"
)

func TestLocalAddrsFromKeepsIPv4AndDropsLoopback(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1")},
		&net.IPNet{IP: net.ParseIP("::1")},
		&net.IPNet{IP: net.ParseIP("fe80::1")},
		&net.IPNet{IP: net.ParseIP("192.168.1.5")},
		&net.IPNet{IP: net.ParseIP("10.0.5.7")},
	}

	got := localAddrsFrom(addrs)

	want := []string{"192.168.1.5", "10.0.5.7"}
	if len(got) != len(want) {
		t.Fatalf("localAddrsFrom = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("localAddrsFrom = %v, want %v", got, want)
		}
	}
}

// A device with no interface but the loopback has no address another device can
// reach, so the list is empty rather than carrying 127.0.0.1.
func TestLocalAddrsFromLoopbackOnly(t *testing.T) {
	addrs := []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1")}}

	if got := localAddrsFrom(addrs); len(got) != 0 {
		t.Fatalf("localAddrsFrom = %v, want no address", got)
	}
}

// An IPv4-mapped IPv6 address is an IPv4 address: it is what a listener bound
// to every interface reports, and dropping it would let the device list itself.
func TestLocalAddrsFromKeepsIPv4Mapped(t *testing.T) {
	addrs := []net.Addr{&net.IPNet{IP: net.ParseIP("::ffff:192.168.1.5")}}

	got := localAddrsFrom(addrs)
	if len(got) != 1 || got[0] != "192.168.1.5" {
		t.Fatalf("localAddrsFrom = %v, want [192.168.1.5]", got)
	}
}

func TestLocalAddrsFromIgnoresOtherAddressTypes(t *testing.T) {
	addrs := []net.Addr{
		&net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 8080},
		&net.IPNet{IP: net.ParseIP("192.168.1.6")},
	}

	got := localAddrsFrom(addrs)
	if len(got) != 1 || got[0] != "192.168.1.6" {
		t.Fatalf("localAddrsFrom = %v, want [192.168.1.6]", got)
	}
}
