package app

import "github.com/fiwon123/cthrone/internal/discovery"

// Data is what the commands hand to the transports.
type Data struct {
	// Port is the port the transport listens on or connects to.
	Port int

	// Host is the interface the websocket host binds to. An empty value
	// means every interface, which is what a chat between two devices on
	// the same network needs.
	Host string

	// Name is the device name announced on the network.
	Name string
}

func New(port int) *Data {
	return &Data{
		Port: port,
	}
}

// ResolveName returns the device name to use: the configured one, or the name
// this device keeps on disk when none was given.
//
// Pairing pins a peer by its name, so a run without --name still has one: the
// hostname identity the network scan already announces. An unnamed device
// would be pinned by its fingerprint alone, and a device that came back with a
// new key would look like a new device instead of a refusal.
func (d *Data) ResolveName() string {
	if d.Name != "" {
		return d.Name
	}

	identity, err := discovery.LoadIdentity()
	if err != nil {
		return ""
	}

	return identity.Name
}
