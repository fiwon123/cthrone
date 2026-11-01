package bluetooth

// Peer is a device a scan found.
//
// It lives beside the framing rather than beside the GATT code because it is
// what a scan reports, and a caller that reads a peer — the terminal printing a
// line, the GUI filling a list — does not need the radio to compile.
type Peer struct {
	// Address is the Bluetooth address, as it is printed and dialled.
	Address string

	// Name is what the device advertises, which for a cthrone host is the
	// name it was given. It is empty for a device that advertises none.
	Name string

	// RSSI is the signal strength in dBm, negative and larger is closer.
	RSSI int16
}

// String is the peer as the terminal prints it: the name, and the address
// beside it, because two devices may share a name.
func (p Peer) String() string {
	if p.Name == "" {
		return p.Address
	}

	return p.Name + " (" + p.Address + ")"
}
