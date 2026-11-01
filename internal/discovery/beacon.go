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

// BeaconPort is the fixed UDP port beacons are sent to and read from. It is
// not the chat port: a beacon says where the chat port is.
const BeaconPort = 9999

// Announcer broadcasts beacons on UDP, and announces to named hosts as well.
//
// The broadcast is what finds this device on its own segment; it never crosses
// a router, which is why two Wi-Fis on one modem do not see each other. The
// seeds are the answer that does: the same beacon, sent by unicast to a host
// that has been reached before, travels wherever ordinary routing does.
type Announcer struct {
	mu    sync.RWMutex
	name  string
	port  int
	seeds []string
	stop  chan struct{}

	// stopOnce makes Stop idempotent: a host that is stopped twice — the Stop
	// button, then the window closing — must not close the channel twice and
	// panic the caller's goroutine.
	stopOnce sync.Once
}

// NewAnnouncer is an announcer for a host on port. seeds are hosts — an address
// or a host:port, as they were dialed — to announce to by unicast.
func NewAnnouncer(name string, port int, seeds ...string) *Announcer {
	return &Announcer{name: name, port: port, seeds: seeds, stop: make(chan struct{})}
}

func (a *Announcer) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		broadcast, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4bcast, Port: BeaconPort})
		if err == nil {
			defer broadcast.Close()
		}

		for {
			select {
			case <-a.stop:
				return
			case <-ticker.C:
				b := Beacon{Name: a.name, Port: a.port, Protocol: 1}
				data, _ := json.Marshal(b)

				if broadcast != nil {
					_, _ = broadcast.Write(data)
				}

				a.announceToSeeds(data)
			}
		}
	}()
}

// announceToSeeds sends the beacon to each named host.
//
// A seed that cannot be reached is skipped in silence: the other segment is
// often down, or the address is stale, and a host that complained about it
// every two seconds would fill a status line with noise nobody can act on.
func (a *Announcer) announceToSeeds(data []byte) {
	a.mu.RLock()
	seeds := append([]string(nil), a.seeds...)
	a.mu.RUnlock()

	for _, seed := range seeds {
		host, _, err := net.SplitHostPort(seed)
		if err != nil {
			host = seed
		}

		if host == "" {
			continue
		}

		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
			IP:   net.ParseIP(host),
			Port: BeaconPort,
		})
		if err != nil {
			continue
		}

		_, _ = conn.Write(data)
		_ = conn.Close()
	}
}

// Seeds are the hosts this announcer also reaches by unicast.
func (a *Announcer) Seeds() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return append([]string(nil), a.seeds...)
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
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: BeaconPort}
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
