//go:build (linux || windows) && !android

// The GATT layer speaks whatever stack tinygo.org/x/bluetooth selects for the
// platform: BlueZ over the D-Bus system bus on Linux, WinRT on Windows. The
// calls are the same for both — the library is the seam — so this file is built
// for either.
//
// Android satisfies the `linux` build tag as well, so it has to be excluded
// explicitly: a phone has no BlueZ and no system bus, and compiling this file
// there would turn "this build has no Bluetooth transport" into a D-Bus failure
// at run time. It needs its own binding to the Java Bluetooth APIs (#206).
//
// **The Windows path compiles and has never been run.** No Windows machine and
// no radio exist in the environment this was written in, and the backends
// differ where it matters: how an address is spelled, whether a disconnect is
// reported at all, the MTU, and how notifications are subscribed. What is
// proven here is that `GOOS=windows go build ./...` produces it; what it does
// when a user runs it is #215's open question.

package bluetooth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// The identifiers the transport speaks. One service, two characteristics: the
// central writes packets into rx, and the peripheral announces them on tx.
//
// They are fixed values rather than a name lookup because a device that is
// scanning cannot ask a question first: the advertisement carries the service,
// and that is the whole of the introduction. The chat's identity is the device
// key, proved in the handshake above this layer, so the service only has to say
// "cthrone", never "which cthrone".
//
// The values are arbitrary and must never change: a peer left on an older
// build simply stops finding this one, which is exactly what an incompatible
// protocol should do.
const (
	serviceUUIDString = "6f1c1f9a-2a3f-4b0e-9a7e-3f5c2b1d0a11"
	rxUUIDString      = "6f1c1f9a-2a3f-4b0e-9a7e-3f5c2b1d0a12"
	txUUIDString      = "6f1c1f9a-2a3f-4b0e-9a7e-3f5c2b1d0a13"

	// PacketSize is the largest packet the transport sends.
	//
	// 20 bytes is what ATT carries before the MTU is negotiated: one
	// notification has a 3-byte header, and the default MTU is 23. The
	// library does not expose MTU negotiation on Linux, so the transport
	// stays at the size every peer can accept rather than guessing a larger
	// one. It costs throughput and buys reliability.
	PacketSize = 20

	// quietGap is how long a host waits after a device leaves before it takes
	// the next one. A BLE link has no disconnect event on Linux, so packets
	// from the device that just left can still be in flight; without the gap
	// they would be read as the first packets of the next device's session.
	quietGap = 2 * time.Second

	// acceptTimeout bounds one wait for a device. A host that prints nothing
	// for a minute looks broken, so the wait is reported and retried.
	acceptTimeout = 60 * time.Second
)

// Errors a caller turns into a message.
var (
	// ErrUnavailable is a machine with no usable adapter: no radio, or a
	// BlueZ that is not running, or a user without permission to use it.
	ErrUnavailable = errors.New("bluetooth: no usable bluetooth adapter")

	// ErrNoDevice is a scan that found nothing to connect to.
	ErrNoDevice = errors.New("bluetooth: no cthrone device found")

	// ErrNotice is the listener being closed while a device was awaited.
	ErrNotice = errors.New("bluetooth: the host stopped listening")
)

// uuids parses the identifiers once per call; a malformed constant is a
// programming error, and it is reported rather than panicking in a library
// nobody expects to panic.
func uuids() (service, rx, tx bluetooth.UUID, err error) {
	if service, err = bluetooth.ParseUUID(serviceUUIDString); err != nil {
		return service, rx, tx, fmt.Errorf("bluetooth: service uuid: %w", err)
	}

	if rx, err = bluetooth.ParseUUID(rxUUIDString); err != nil {
		return service, rx, tx, fmt.Errorf("bluetooth: rx uuid: %w", err)
	}

	if tx, err = bluetooth.ParseUUID(txUUIDString); err != nil {
		return service, rx, tx, fmt.Errorf("bluetooth: tx uuid: %w", err)
	}

	return service, rx, tx, nil
}

// gattLink is one open BLE pipe, in either role. The role lives in the two
// function fields: a peripheral notifies on its tx characteristic, a central
// writes to the peer's rx characteristic.
type gattLink struct {
	name string
	mtu  int

	write      func(packet []byte) error
	disconnect func() error

	incoming chan []byte

	// writeMu serializes packets. The session serializes whole messages, but
	// the keepalive writes a probe from its own goroutine, and two GATT writes
	// interleaved on one characteristic would be read as one malformed packet.
	writeMu sync.Mutex

	mu        sync.Mutex
	reason    string
	closed    bool
	closeOnce sync.Once
}

func newGattLink(name string, write func([]byte) error, disconnect func() error) *gattLink {
	return &gattLink{
		name:       name,
		mtu:        PacketSize,
		write:      write,
		disconnect: disconnect,
		incoming:   make(chan []byte, 64),
	}
}

func (l *gattLink) Name() string { return l.name }

func (l *gattLink) MTU() int { return l.mtu }

func (l *gattLink) Incoming() <-chan []byte { return l.incoming }

func (l *gattLink) Reason() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.reason
}

func (l *gattLink) Write(packet []byte) error {
	if len(packet) > l.mtu {
		return fmt.Errorf("%w: %d bytes for a packet of %d", ErrMessage, len(packet), l.mtu)
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	return l.write(packet)
}

func (l *gattLink) Close(reason string) error {
	l.end(reason)

	return nil
}

// deliver hands a packet to the reader. A reader that has fallen behind is a
// link that has failed: the alternative is to block the radio's callback
// goroutine, which stops every other packet too.
func (l *gattLink) deliver(packet []byte) {
	select {
	case l.incoming <- packet:
	default:
		l.end("bluetooth: the device sent faster than it could be read")
	}
}

// end records why the link is over, closes the reader, and tells the transport
// so it can free the slot or hang up.
func (l *gattLink) end(reason string) {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.reason = reason
		l.closed = true
		l.mu.Unlock()

		close(l.incoming)

		if l.disconnect != nil {
			_ = l.disconnect()
		}
	})
}

// Listener is a host that advertises the service and serves one device at a
// time.
type Listener struct {
	adapter *bluetooth.Adapter
	service *bluetooth.Service
	adv     *bluetooth.Advertisement
	tx      *bluetooth.Characteristic

	// packets carries what arrived while no device was attached: the hello of
	// a device that has just connected. Buffered, because the writer is the
	// D-Bus dispatcher and must not block on us.
	packets chan []byte

	done      chan struct{}
	closeOnce sync.Once

	mu         sync.Mutex
	current    *gattLink
	quietUntil time.Time
	closed     bool
}

// Listen enables the adapter, publishes the service, and starts advertising.
func Listen(name string) (*Listener, error) {
	service, rx, tx, err := uuids()
	if err != nil {
		return nil, err
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	l := &Listener{
		adapter: adapter,
		packets: make(chan []byte, 128),
		done:    make(chan struct{}),
	}

	// The tx handle is pre-created so the host can notify after AddService
	// has filled it in.
	txCharacteristic := &bluetooth.Characteristic{}

	svc := &bluetooth.Service{
		UUID: service,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				UUID:       rx,
				Flags:      bluetooth.CharacteristicWriteWithoutResponsePermission | bluetooth.CharacteristicWritePermission,
				WriteEvent: l.onWrite,
			},
			{
				Handle: txCharacteristic,
				UUID:   tx,
				Flags:  bluetooth.CharacteristicNotifyPermission,
			},
		},
	}

	if err := adapter.AddService(svc); err != nil {
		return nil, fmt.Errorf("bluetooth: publishing the service: %w", err)
	}

	adv := adapter.DefaultAdvertisement()
	if err := adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    name,
		ServiceUUIDs: []bluetooth.UUID{service},
	}); err != nil {
		_ = adapter.RemoveService(svc)

		return nil, fmt.Errorf("bluetooth: configuring the advertisement: %w", err)
	}

	if err := adv.Start(); err != nil {
		_ = adapter.RemoveService(svc)

		return nil, fmt.Errorf("bluetooth: advertising: %w", err)
	}

	l.service = svc
	l.adv = adv
	l.tx = txCharacteristic

	return l, nil
}

// onWrite is every packet a central writes. BlueZ does not say which central
// wrote it, which is why this host serves one device at a time.
func (l *Listener) onWrite(_ bluetooth.Connection, _ int, value []byte) {
	packet := append([]byte(nil), value...)

	l.mu.Lock()

	if l.closed {
		l.mu.Unlock()

		return
	}

	if link := l.current; link != nil {
		l.mu.Unlock()
		link.deliver(packet)

		return
	}

	quiet := time.Now().Before(l.quietUntil)
	l.mu.Unlock()

	if quiet {
		return
	}

	select {
	case l.packets <- packet:
	default:
		// Nobody is reading and the queue is full. The device will send its
		// hello again when the user reconnects.
	}
}

// Accept waits for a device and returns the link to talk to it.
//
// A BLE peripheral on Linux is told nothing about connections, so "a device
// attached" is "a packet arrived": the first packet of a fresh session is the
// peer's hello, and it is the only signal available.
func (l *Listener) Accept() (Link, error) {
	for {
		l.mu.Lock()
		closed := l.closed
		quietUntil := l.quietUntil
		l.mu.Unlock()

		if closed {
			return nil, ErrNotice
		}

		// Discard what the device that just left left behind.
		if wait := time.Until(quietUntil); wait > 0 {
			select {
			case <-l.packets:
			case <-time.After(wait):
			case <-l.done:
				return nil, ErrNotice
			}

			continue
		}

		var first []byte

		select {
		case packet := <-l.packets:
			first = packet
		case <-time.After(acceptTimeout):
			return nil, fmt.Errorf("%w: nothing connected in %s", ErrNoDevice, acceptTimeout)
		case <-l.done:
			return nil, ErrNotice
		}

		var link *gattLink

		link = newGattLink("", l.notify, func() error {
			l.release(link)

			return nil
		})

		l.mu.Lock()

		if l.closed {
			l.mu.Unlock()

			return nil, ErrNotice
		}

		// The packet may have arrived during the quiet gap after all.
		if time.Now().Before(l.quietUntil) {
			l.mu.Unlock()

			continue
		}

		l.current = link
		l.mu.Unlock()

		link.deliver(first)

		// Anything else the device sent while we were setting up belongs to
		// this session.
		for {
			select {
			case packet := <-l.packets:
				link.deliver(packet)
			default:
				return link, nil
			}
		}
	}
}

// notify announces a packet on the tx characteristic. BlueZ sends it to every
// central subscribed to the characteristic, which is why a host is one device
// at a time.
func (l *Listener) notify(packet []byte) error {
	if l.tx == nil {
		return errors.New("bluetooth: the host is not listening")
	}

	if _, err := l.tx.Write(packet); err != nil {
		return fmt.Errorf("bluetooth: notifying: %w", err)
	}

	return nil
}

// release frees the slot for the next device, and starts the quiet gap so the
// next device's hello is not read as the tail of this one's session.
func (l *Listener) release(link *gattLink) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.current == link {
		l.current = nil
	}

	l.quietUntil = time.Now().Add(quietGap)
}

// Close stops advertising and serving.
func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		link := l.current
		l.current = nil
		l.mu.Unlock()

		if link != nil {
			link.end("bluetooth: the host stopped")
		}

		close(l.done)

		if l.adv != nil {
			_ = l.adv.Stop()
		}

		if l.service != nil {
			_ = l.adapter.RemoveService(l.service)
		}
	})

	return nil
}

// Dial finds a cthrone device, connects to it, and returns the link.
//
// target is what the user named: a Bluetooth address, a device name, or empty
// for the first cthrone device the scan finds.
func Dial(target string, timeout time.Duration) (Link, error) {
	service, rx, tx, err := uuids()
	if err != nil {
		return nil, err
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	peer, err := find(adapter, service, target, timeout)
	if err != nil {
		return nil, err
	}

	device, err := adapter.Connect(peer.address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("bluetooth: connecting to %s: %w", peer.name, err)
	}

	services, err := device.DiscoverServices([]bluetooth.UUID{service})
	if err != nil || len(services) == 0 {
		_ = device.Disconnect()

		return nil, fmt.Errorf("bluetooth: %s does not offer the cthrone service: %w", peer.name, err)
	}

	characteristics, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{rx, tx})
	if err != nil {
		_ = device.Disconnect()

		return nil, fmt.Errorf("bluetooth: reading the characteristics of %s: %w", peer.name, err)
	}

	var rxChar, txChar *bluetooth.DeviceCharacteristic

	for i := range characteristics {
		switch characteristics[i].UUID().String() {
		case rxUUIDString:
			rxChar = &characteristics[i]
		case txUUIDString:
			txChar = &characteristics[i]
		}
	}

	if rxChar == nil || txChar == nil {
		_ = device.Disconnect()

		return nil, fmt.Errorf("bluetooth: %s is missing the chat characteristics", peer.name)
	}

	link := newGattLink(peer.name, func(packet []byte) error {
		if _, err := rxChar.WriteWithoutResponse(packet); err != nil {
			return fmt.Errorf("bluetooth: writing: %w", err)
		}

		return nil
	}, device.Disconnect)

	if err := txChar.EnableNotifications(func(buf []byte) {
		link.deliver(append([]byte(nil), buf...))
	}); err != nil {
		_ = device.Disconnect()

		return nil, fmt.Errorf("bluetooth: subscribing to %s: %w", peer.name, err)
	}

	return link, nil
}

// hit is what a scan found: the address to dial and what to call it.
type hit struct {
	address bluetooth.Address
	name    string
}

// find scans until it sees a cthrone device that matches target.
func find(adapter *bluetooth.Adapter, service bluetooth.UUID, target string, timeout time.Duration) (hit, error) {
	found := make(chan hit, 1)

	scanErr := make(chan error, 1)

	go func() {
		scanErr <- adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
			if !result.HasServiceUUID(service) {
				return
			}

			name := result.LocalName()
			if !matches(target, result.Address.String(), name) {
				return
			}

			select {
			case found <- hit{address: result.Address, name: name}:
			default:
			}

			// The scan runs until it is stopped; one answer is all we need.
			_ = a.StopScan()
		})
	}()

	defer func() {
		_ = adapter.StopScan()
	}()

	select {
	case h := <-found:
		return h, nil

	case err := <-scanErr:
		if err != nil {
			return hit{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}

		return hit{}, ErrNoDevice

	case <-time.After(timeout):
		return hit{}, fmt.Errorf("%w: nothing in %s", ErrNoDevice, timeout)
	}
}

// matches reports whether a scanned device is the one the user named. An empty
// target takes the first cthrone device, an address-like target is compared as
// an address, and anything else is compared with the advertised name, ignoring
// case: a name is typed by a person and a MAC is copied.
func matches(target, address, name string) bool {
	if target == "" {
		return true
	}

	if strings.EqualFold(target, address) {
		return true
	}

	if strings.EqualFold(target, name) {
		return true
	}

	// A name may carry a suffix the user did not type, and a Bluetooth address
	// is written with colons or dashes. Accepting both is what keeps a typo
	// from looking like a device that is not there.
	return strings.Contains(strings.ToLower(name), strings.ToLower(target))
}

// Scan lists the cthrone devices in range, calling report for each one.
func Scan(timeout time.Duration, report func(Peer)) error {
	service, _, _, err := uuids()
	if err != nil {
		return err
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	seen := make(map[string]bool)

	var mu sync.Mutex

	scanErr := make(chan error, 1)

	go func() {
		scanErr <- adapter.Scan(func(_ *bluetooth.Adapter, result bluetooth.ScanResult) {
			if !result.HasServiceUUID(service) {
				return
			}

			address := result.Address.String()
			name := result.LocalName()

			mu.Lock()
			if seen[address] {
				mu.Unlock()

				return
			}

			seen[address] = true
			mu.Unlock()

			report(Peer{Address: address, Name: name, RSSI: result.RSSI})
		})
	}()

	select {
	case <-time.After(timeout):
	case err := <-scanErr:
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
	}

	_ = adapter.StopScan()

	return nil
}
