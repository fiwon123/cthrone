package discovery

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

// Beacon is announced by devices on the LAN.
type Beacon struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol int    `json:"protocol"`
	Addr     string `json:"addr,omitempty"`
}

// Announcer broadcasts beacons on UDP.
type Announcer struct {
	mu   sync.RWMutex
	name string
	port int
	stop chan struct{}
}

func NewAnnouncer(name string, port int) *Announcer {
	return &Announcer{name: name, port: port, stop: make(chan struct{})}
}

func (a *Announcer) Start(interval time.Duration) {
	go func() {
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4bcast, Port: 9999})
		if err != nil {
			// try interface broadcasts
			return
		}
		defer conn.Close()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-a.stop:
				return
			case <-ticker.C:
				b := Beacon{Name: a.name, Port: a.port, Protocol: 1}
				data, _ := json.Marshal(b)
				conn.Write(data)
			}
		}
	}()
}

func (a *Announcer) Stop() {
	close(a.stop)
}

// Listener listens for beacons and reports discovered devices.
type Listener struct {
	mu      sync.RWMutex
	found   map[string]Beacon
	onFound func(Beacon)
}

func NewListener() *Listener {
	return &Listener{found: make(map[string]Beacon)}
}

func (l *Listener) OnFound(fn func(Beacon)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onFound = fn
}

func (l *Listener) Start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				return
			}
		}()
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: 9999}
		conn, err := net.ListenUDP("udp4", addr)
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var b Beacon
			if err := json.Unmarshal(buf[:n], &b); err != nil {
				continue
			}
			if b.Addr == "" {
				b.Addr = raddr.IP.String()
			}
			l.mu.Lock()
			key := b.Addr + ":" + itoa(b.Port)
			l.found[key] = b
			fn := l.onFound
			l.mu.Unlock()
			if fn != nil {
				fn(b)
			}
		}
	}()
}

func (l *Listener) Found() []Beacon {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Beacon, 0, len(l.found))
	for _, v := range l.found {
		out = append(out, v)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [32]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
