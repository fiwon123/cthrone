package app

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
