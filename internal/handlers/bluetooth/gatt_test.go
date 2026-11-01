//go:build (linux || windows) && !android

package bluetooth

import "testing"

// Naming a peer is how `--bluetooth <name|address>` narrows a scan, and the
// address a user types is rarely the one the scan prints: this program writes
// "AA:BB:CC:DD:EE:FF", Windows' own device list writes
// "AA-BB-CC-DD-EE-FF", and a paste from a bug report may carry no separator at
// all. All three name the same device, so all three have to match — a device
// that is present but not found reads as a broken scan.
func TestMatches(t *testing.T) {
	const (
		address = "AA:BB:CC:DD:EE:FF"
		name    = "laptop"
	)

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"nothing named finds everything", "", true},
		{"the address as this program prints it", address, true},
		{"the address in lower case", "aa:bb:cc:dd:ee:ff", true},
		{"the address as Windows prints it", "AA-BB-CC-DD-EE-FF", true},
		{"the address with no separators", "aabbccddeeff", true},
		{"the address with spaces", "AA BB CC DD EE FF", true},
		{"the device name", name, true},
		{"the device name in another case", "LAPTOP", true},
		{"a prefix of the name", "lap", true},
		{"a different device's address", "AA:BB:CC:DD:EE:00", false},
		{"a name that is not in the advertised one", "desktop", false},
		{"part of an address, which is not a name", "AA:BB", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matches(tt.target, address, name); got != tt.want {
				t.Errorf("matches(%q, %q, %q) = %v, want %v", tt.target, address, name, got, tt.want)
			}
		})
	}
}

// Two addresses that fold to nothing must not compare equal: an empty address
// on one side would otherwise match every target that is also only separators.
func TestSameAddressRefusesNothing(t *testing.T) {
	if sameAddress("--::", "") {
		t.Error("two empty addresses compared equal, so any target would match")
	}

	if sameAddress(":", "-") {
		t.Error("two addresses with no digits compared equal")
	}
}
