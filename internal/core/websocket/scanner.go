package websocketcore

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
)

// Scan all available local ips
func Scan(app *app.Data) {
	myIP, err := getLocalIP()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("MyLocalIP: ", myIP)
	mySubnet := subnetFromIP(myIP)
	timeout := 300 * time.Millisecond

	fmt.Println("Scanning...")
	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("%s%d", mySubnet, i)
		go func(ip string) {
			conn, err := net.DialTimeout("tcp", ip+":"+strconv.Itoa(app.Port), timeout)
			if err == nil {
				fmt.Println("Found chat server at:", ip)
				conn.Close()
			}
		}(ip)
	}

	fmt.Println("Done")

	time.Sleep(1 * time.Second)
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			ip := ipnet.IP
			if ip.To4() != nil && !ip.IsLoopback() {

				if strings.Contains(ip.String(), "192.168.") {
					return ip.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no local IP found")
}

func subnetFromIP(ip string) string {
	lastDot := strings.LastIndex(ip, ".")
	return ip[:lastDot+1]
}
