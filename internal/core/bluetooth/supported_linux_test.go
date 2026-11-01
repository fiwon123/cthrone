//go:build linux && !android

package bluetoothcore

import "testing"

// A Linux build carries the transport, so it must say so: the GUI's transport
// select and the CLI both read this answer, and a false one would hide a radio
// the binary really has.
func TestSupported(t *testing.T) {
	if !Supported() {
		t.Fatal("Supported() = false on a build that compiles the BlueZ transport")
	}
}
