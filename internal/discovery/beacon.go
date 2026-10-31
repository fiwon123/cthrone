package discovery

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

// BeaconTTL is how long a beacon is trusted after it was last heard. A host
// announces every two seconds, so this is several missed announcements: long
// enough that a busy network does not make a live device blink, short enough
// that a stopped host leaves the list.
const BeaconTTL = 12 * time.Second

// Beacon is announced by devices on the LAN.
type Beacon struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol int    `json:"protocol"`
	Addr     string `json:"addr,omitempty"`
}

// entry is a beacon and when it was last heard.
type entry struct {
	beacon Beacon
	seen   time.Time
}

// Announcer broadcasts beacons on UDP.
type Announcer struct {
	mu   sync.RWMutex
	name string
	port int
	stop chan struct{}

	// stopOnce makes Stop idempotent: a host that is stopped twice — the Stop
	// button, then the window closing — must not close the channel twice and
	// panic the caller's goroutine.
	stopOnce sync.Once
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

// Stop ends the announcement. It is safe to call more than once.
func (a *Announcer) Stop() {
	a.stopOnce.Do(func() { close(a.stop) })
}

// Listener listens for beacons and reports discovered devices.
type Listener struct {
	mu      sync.RWMutex
	found   map[string]entry
	onFound func(Beacon)
	now     func() time.Time
}

func NewListener() *Listener {
	return &Listener{found: make(map[string]entry), now: time.Now}
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
			l.observe(b, l.now())
		}
	}()
}

// observe records one hearing of a beacon and reports it. Every hearing is
// news — the caller refreshes how recently the device was seen — while a
// device heard twice stays one entry.
func (l *Listener) observe(b Beacon, at time.Time) {
	l.mu.Lock()
	l.found[beaconKey(b)] = entry{beacon: b, seen: at}
	fn := l.onFound
	l.mu.Unlock()

	if fn != nil {
		fn(b)
	}
}

// Found returns the devices heard within BeaconTTL.
func (l *Listener) Found() []Beacon {
	return l.Prune(l.now(), BeaconTTL)
}

// Prune drops the beacons last heard more than ttl ago and returns the rest.
// It is the expiry that keeps a stopped host from being listed forever.
func (l *Listener) Prune(now time.Time, ttl time.Duration) []Beacon {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]Beacon, 0, len(l.found))
	for key, e := range l.found {
		if now.Sub(e.seen) > ttl {
			delete(l.found, key)
			continue
		}
		out = append(out, e.beacon)
	}

	return out
}

func beaconKey(b Beacon) string {
	return b.Addr + ":" + itoa(b.Port)
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
