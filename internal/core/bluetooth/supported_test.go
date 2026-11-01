//go:build (linux || windows) && !android

package bluetoothcore

import "testing"

// A build with a radio backend carries the transport, so it must say so: the
// GUI's transport select and the CLI both read this answer, and a false one
// would hide a radio the binary really has.
//
// The backend differs by platform — BlueZ over D-Bus on Linux, WinRT on
// Windows — and this answer is the same for both.
func TestSupported(t *testing.T) {
	if !Supported() {
		t.Fatal("Supported() = false on a build that compiles a bluetooth backend")
	}
}
