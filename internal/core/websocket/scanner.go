package websocketcore

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
)

var localIP = getLocalIP

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

// Scan all available local ips
func Scan(app *app.Data) []string {
	myIP, err := localIP()
	if err != nil {
		fmt.Println(err)
		return nil
	}

	fmt.Println("MyLocalIP: ", myIP)
	mySubnet := subnetFromIP(myIP)
	timeout := 300 * time.Millisecond

	// A host answers on its own addresses, so a scan that kept every answer
	// would list this device as a peer to connect to. Every address the machine
	// holds is dropped, not just the one the subnet walk starts from: a host
	// bound to every interface answers on each of them.
	own := make(map[string]bool)
	for _, ip := range localAddrs() {
		own[ip] = true
	}
	own[myIP] = true

	fmt.Println("Scanning...")

	var mu sync.Mutex
	var found []string
	var wg sync.WaitGroup

	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("%s%d", mySubnet, i)
		if own[ip] {
			// This device: it answers on its own address, and it is not a peer.
			continue
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			err := dialScan(ip+":"+strconv.Itoa(app.Port), timeout)
			if err == nil {
				mu.Lock()
				found = append(found, ip)
				mu.Unlock()
				fmt.Println("Found chat server at:", ip)
			}
		}(ip)
	}

	wg.Wait()
	sort.Strings(found)

	fmt.Println("Done")
	return found
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
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
