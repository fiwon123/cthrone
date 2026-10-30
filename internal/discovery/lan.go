package discovery

import (
	"net"
	"strings"
)

// probeAddr is where the route probe points: TEST-NET-1 (RFC 5737) at the
// discard port. The target never matters — connecting a UDP socket makes the
// kernel pick a source address and sends no packet — so the address is chosen to
// be one that no real device answers and no one could mistake for a peer.
const probeAddr = "192.0.2.1:9"

// dialRoute opens the probe socket. It is a variable so a test supplies the
// answer instead of asking the machine's routing table.
var dialRoute = func() (net.Conn, error) {
	return net.Dial("udp4", probeAddr)
}

// DefaultRouteIP returns the address this device's default route uses, which is
// the network it is actually on.
//
// net.InterfaceAddrs lists every address in an unspecified order, so taking the
// first private one picks a docker bridge, a VPN adapter or a virtual machine's
// host-only network as often as the LAN: the scan then walks a subnet no peer is
// on, and the device advertises an address no peer can reach. The route's own
// source address is the one a peer on this network would see.
//
// A device with no default route — an isolated LAN, a host that is only
// reachable from its own subnet — reports false, and the caller falls back to
// the address list.
func DefaultRouteIP() (string, bool) {
	conn, err := dialRoute()
	if err != nil {
		return "", false
	}
	defer conn.Close()

	udp, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udp.IP == nil {
		return "", false
	}

	ip := udp.IP
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		return "", false
	}

	return ip.String(), true
}

// AdvertisedAddrs returns the addresses a peer on preferred's network can
// reach, with preferred first.
//
// The scan walks one /24 — subnetFromIP in the websocket scanner — so an
// address in another /24 is not a peer's way in: it is a docker bridge, a VPN
// adapter or a second NIC the device happens to hold. Listing it tells the other
// device to dial an address that will not answer, so it is left out.
//
// With no preferred address there is no network to judge against, and every
// address is returned in the order the machine gave them, which is what the
// scanner and the GUI did before the route was known.
func AdvertisedAddrs(addrs []string, preferred string) []string {
	if preferred == "" {
		return append([]string(nil), addrs...)
	}

	if len(addrs) == 0 {
		return nil
	}

	out := make([]string, 0, len(addrs))
	out = append(out, preferred)

	for _, addr := range addrs {
		if addr == preferred || !sameSubnet24(addr, preferred) {
			continue
		}

		out = append(out, addr)
	}

	return out
}

// sameSubnet24 reports whether a and b share a /24, the unit the scan walks.
func sameSubnet24(a, b string) bool {
	prefix := subnet24(a)

	return prefix != "" && prefix == subnet24(b)
}

// subnet24 is the /24 prefix of an IPv4 address, in the form the scan dials.
func subnet24(ip string) string {
	i := strings.LastIndexByte(ip, '.')
	if i < 0 {
		return ""
	}

	return ip[:i+1]
}
