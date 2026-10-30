package discovery

import "net"

// LocalAddrs returns every non-loopback IPv4 address this device holds.
//
// A host answers on each of them, and the beacon it announces can be heard
// back when the operating system loops the broadcast — so without this list a
// device lists itself as a peer on the network. The loopback is left out
// because no other device can reach it, and IPv6 because the transports and
// the scan speak IPv4.
func LocalAddrs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	return localAddrsFrom(addrs)
}

// localAddrsFrom is the half of LocalAddrs that can be tested without the
// machine's interfaces: it keeps the IPv4 addresses a device answers on and
// drops the loopback and the IPv6 ones.
func localAddrsFrom(addrs []net.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		ip := ipnet.IP
		if ip.To4() == nil || ip.IsLoopback() {
			continue
		}

		out = append(out, ip.String())
	}

	return out
}
