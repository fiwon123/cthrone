//go:build linux && !android

package bluetoothcore

import (
	"fmt"
	"io"
	"time"

	bluetoothhandler "github.com/fiwon123/cthrone/internal/handlers/bluetooth"
)

// listScan walks the radio for devices that offer the chat service. It is a
// variable so a test does not wait out a scan.
var listScan = bluetoothhandler.Scan

// ScanPeers lists the Bluetooth devices in range, calling report for each one.
//
// It is the same walk as Scan with the reporting left to the caller: the
// terminal prints a line, the GUI puts the peer in its Devices list, and both
// read one scan instead of one each.
func ScanPeers(timeout time.Duration, report func(Peer)) error {
	return listScan(timeout, report)
}

// Scan lists the Bluetooth devices in range.
//
// There is no beacon to listen for on this transport, so a scan is the only
// discovery there is: the host advertises the service and the scan reports what
// advertises it. The name is the host's, and the address is what `connect`
// takes when two devices carry the same name.
func Scan(out io.Writer) error {
	fmt.Fprintln(out, "Scanning for Bluetooth devices — the radio reaches about ten metres")

	found := 0

	err := ScanPeers(scanTimeout, func(peer Peer) {
		found++

		fmt.Fprintf(out, "  %s\n", peer)
	})
	if err != nil {
		return err
	}

	if found == 0 {
		fmt.Fprintln(out, "No cthrone device answered over Bluetooth")
		fmt.Fprintln(out, "  a device only answers while it is hosting, and only within radio range")

		return nil
	}

	fmt.Fprintf(out, "Found %d Bluetooth device(s)\n", found)

	return nil
}
