package websocketcore

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
)

const (
	// defaultScanTimeout is how long one dial may take. A device on the same
	// segment answers in milliseconds; 300ms is long enough for a busy Wi-Fi
	// and short enough that a /24 finishes in a few seconds.
	defaultScanTimeout = 300 * time.Millisecond

	// scanWorkers bounds the dials in flight. A /24 is 254 addresses and the
	// common case; the pool is what keeps a wider walk from opening thousands
	// of sockets at once.
	scanWorkers = 128

	// MaxScanHosts bounds one scan run. 4096 addresses still covers a /20, so
	// a network that really is wider than a /24 can be walked, while a
	// mistyped /16 — 65,534 dials — is refused with a message instead of
	// running for minutes.
	MaxScanHosts = 4096
)

// ErrTooManyHosts is returned when the requested subnets cover more addresses
// than MaxScanHosts. The message names the limit so the user can narrow the
// prefix instead of guessing why nothing happened.
var ErrTooManyHosts = fmt.Errorf("scan: the requested subnets cover more than %d addresses; use a /20 or narrower", MaxScanHosts)

var localIP = getLocalIP

// interfaceAddrs is every address the machine holds. It is a variable so a
// test supplies the list instead of asking the machine's adapters.
var interfaceAddrs = net.InterfaceAddrs

// defaultRouteIP is the address the default route uses — the network this
// device is actually on. It is a variable so a test supplies the answer.
var defaultRouteIP = discovery.DefaultRouteIP

// localAddrs is every address this device holds, so a scan can tell the host's
// own answers from a peer's. It is a variable so a test supplies the addresses
// instead of asking the machine.
var localAddrs = discovery.LocalAddrs

var dialScan = func(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Scan walks the subnet the default route uses.
//
// This is the behaviour every existing caller expects, and it stays exactly as
// it was: one /24, the one this device is actually on. Use ScanSubnets to walk
// a segment somewhere else.
func Scan(app *app.Data) []string {
	myIP, err := localIP()
	if err != nil {
		fmt.Println(err)
		return nil
	}

	fmt.Println("MyLocalIP: ", myIP)

	prefix, ok := subnetPrefix(myIP)
	if !ok {
		fmt.Println("no local IP found")

		return nil
	}

	fmt.Println("Scanning...")

	found, err := ScanSubnets(app, []netip.Prefix{prefix}, defaultScanTimeout)
	if err != nil {
		fmt.Println(err)

		return nil
	}

	fmt.Println("Done")

	return found
}

// ScanSubnets walks every host address in prefixes and returns the ones that
// answer on the chat port, sorted.
//
// A scan only finds what this device can route to. A segment behind a NAT, or
// one whose router isolates clients, looks exactly like an empty segment: no
// dial answers. The caller is expected to say so rather than report it as
// "nobody is there", because the two have different fixes.
func ScanSubnets(app *app.Data, prefixes []netip.Prefix, timeout time.Duration) ([]string, error) {
	hosts, err := scanHosts(prefixes)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = defaultScanTimeout
	}

	// A host answers on its own addresses, so a scan that kept every answer
	// would list this device as a peer to connect to. Every address the machine
	// holds is dropped, on every subnet walked, not just the one the walk
	// started from: a host bound to every interface answers on each of them.
	own := ownAddresses()

	workers := scanWorkers
	if len(hosts) < workers {
		workers = len(hosts)
	}

	var (
		mu    sync.Mutex
		found []string
		wg    sync.WaitGroup
	)

	jobs := make(chan string)

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for ip := range jobs {
				addr := net.JoinHostPort(ip, strconv.Itoa(app.Port))
				if err := dialScan(addr, timeout); err == nil {
					mu.Lock()
					found = append(found, ip)
					mu.Unlock()
				}
			}
		}()
	}

	for _, ip := range hosts {
		if own[ip] {
			// This device: it answers on its own address, and it is not a peer.
			continue
		}

		jobs <- ip
	}

	close(jobs)
	wg.Wait()

	sort.Strings(found)

	return found, nil
}

// ownAddresses is every address this device holds, so a scan can tell the
// host's own answers from a peer's.
func ownAddresses() map[string]bool {
	own := make(map[string]bool)

	for _, ip := range localAddrs() {
		own[ip] = true
	}

	return own
}

// scanHosts expands prefixes into the addresses to dial.
//
// The total is counted before anything is allocated, so an oversized prefix is
// refused by arithmetic rather than by trying it and running out of memory.
func scanHosts(prefixes []netip.Prefix) ([]string, error) {
	var total uint64

	for _, p := range prefixes {
		if !p.IsValid() {
			return nil, fmt.Errorf("scan: %q is not a valid subnet", p)
		}

		if !p.Addr().Is4() {
			return nil, fmt.Errorf("scan: %s is not IPv4", p)
		}

		total += prefixCount(p.Masked())
		if total > MaxScanHosts {
			return nil, ErrTooManyHosts
		}
	}

	out := make([]string, 0, total)

	for _, p := range prefixes {
		base, offsets := prefixHosts(p.Masked())

		for _, offset := range offsets {
			out = append(out, addrAt(base, offset).String())
		}
	}

	return out, nil
}

// prefixCount is how many addresses of p are worth dialling: every one except
// the network and broadcast addresses, which no host holds. A /31 and a /32
// have neither to skip (RFC 3021), so both are walked whole.
func prefixCount(p netip.Prefix) uint64 {
	total := uint64(1) << uint(32-p.Bits())
	if total > 2 {
		return total - 2
	}

	return total
}

// prefixHosts returns the base address of p and the offsets to dial.
func prefixHosts(p netip.Prefix) (uint32, []uint64) {
	total := uint64(1) << uint(32-p.Bits())
	base := binary.BigEndian.Uint32(p.Addr().AsSlice())

	start, end := uint64(0), total
	if total > 2 {
		start, end = 1, total-1
	}

	offsets := make([]uint64, 0, end-start)
	for i := start; i < end; i++ {
		offsets = append(offsets, i)
	}

	return base, offsets
}

// addrAt is the address base+offset.
func addrAt(base uint32, offset uint64) netip.Addr {
	v := base + uint32(offset)

	var b [4]byte
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)

	return netip.AddrFrom4(b)
}

// subnetPrefix is the /24 an address belongs to.
func subnetPrefix(ip string) (netip.Prefix, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return netip.Prefix{}, false
	}

	return netip.PrefixFrom(addr, 24).Masked(), true
}

// ParseSubnets turns --subnet values into prefixes, refusing anything that is
// not an IPv4 CIDR before a single dial is made.
func ParseSubnets(values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		p, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("scan: %q is not an IPv4 CIDR such as 192.168.1.0/24", value)
		}

		if !p.Addr().Is4() {
			return nil, fmt.Errorf("scan: %q is not IPv4", value)
		}

		out = append(out, p.Masked())
	}

	return out, nil
}

// LocalSubnets returns the /24 of every IPv4 interface this device holds, so a
// machine wired into two segments can scan both.
//
// A narrower interface mask is kept as it is — a /28 really is that small —
// while a wider one is clamped to /24: a home router that hands out a /16 does
// not mean the user wants 65,000 dials, and MaxScanHosts would refuse it
// anyway.
func LocalSubnets() []netip.Prefix {
	addrs, err := interfaceAddrs()
	if err != nil {
		return nil
	}

	var out []netip.Prefix

	seen := make(map[netip.Prefix]bool)

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		ip, ok := netip.AddrFromSlice(ipnet.IP)
		if !ok {
			continue
		}

		ip = ip.Unmap()
		if !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}

		bits := 24
		if ones, _ := ipnet.Mask.Size(); ones > bits {
			bits = ones
		}

		p := netip.PrefixFrom(ip, bits).Masked()
		if seen[p] {
			continue
		}

		seen[p] = true

		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })

	return out
}

// getLocalIP returns the address whose subnet the scan walks: the one the
// default route uses, so a docker bridge or a VPN adapter listed first does not
// send the scan into a subnet no peer is on.
func getLocalIP() (string, error) {
	if ip, ok := defaultRouteIP(); ok {
		return ip, nil
	}

	// No default route — an isolated LAN, or a host reachable only from its own
	// subnet — leaves the address list as the only evidence of where this device
	// is, which is what the scan did before the route was consulted.
	addrs, err := interfaceAddrs()
	if err != nil {
		return "", err
	}

	ip, ok := selectLocalIP(addrs)
	if !ok {
		return "", fmt.Errorf("no local IP found")
	}
	return ip, nil
}

func selectLocalIP(addrs []net.Addr) (string, bool) {
	var fallback string

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.To4() == nil || ip.IsLoopback() {
			continue
		}

		s := ip.String()
		if isRFC1918(s) {
			return s, true
		}
		if fallback == "" {
			fallback = s
		}
	}

	if fallback != "" {
		return fallback, true
	}
	return "", false
}

func isRFC1918(ip string) bool {
	if strings.HasPrefix(ip, "192.168.") {
		return true
	}
	if strings.HasPrefix(ip, "10.") {
		return true
	}
	if strings.HasPrefix(ip, "172.") {
		parts := strings.Split(ip, ".")
		if len(parts) >= 2 {
			var second int
			fmt.Sscanf(parts[1], "%d", &second)
			if second >= 16 && second <= 31 {
				return true
			}
		}
		return false
	}
	return false
}

func subnetFromIP(ip string) string {
	lastDot := strings.LastIndex(ip, ".")
	if lastDot == -1 {
		return ip + "."
	}
	return ip[:lastDot+1]
}
