package app

// Data is what the commands hand to the transports.
type Data struct {
	// Port is the port the transport listens on or connects to.
	Port int

	// Host is the interface the websocket host binds to. An empty value
	// means every interface, which is what a chat between two devices on
	// the same network needs.
	Host string

	// AllowAnonymous lets a client join a session without the shared
	// secret. It is opt-in: a host that requires no credential is the
	// state the tool was in before, and it should never be the state it
	// is in by accident.
	AllowAnonymous bool

	// Name is the device name announced on the network.
	Name string
}

func New(port int) *Data {

	return &Data{
		Port: port,
	}
}
