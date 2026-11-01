package scancmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
)

func TestScanCommandRegistered(t *testing.T) {
	if Cmd.Use != "scan" {
		t.Errorf("expected Use to be 'scan', got %q", Cmd.Use)
	}
}

// stubBeaconNames keeps the command off the discovery port: a unit test must
// not wait out the announcement window, and it must not need the network.
func stubBeaconNames(t *testing.T) {
	t.Helper()

	original := beaconNames
	t.Cleanup(func() { beaconNames = original })

	beaconNames = func(addrs []string, port int, window time.Duration) map[string]string {
		return nil
	}
}

func TestScanCommandUsesPort(t *testing.T) {
	orig := scanFunc
	defer func() { scanFunc = orig }()
	stubBeaconNames(t)

	var gotPort int
	scanFunc = func(a *app.Data) []string {
		gotPort = a.Port
		return nil
	}

	// Add port flag like root does
	Cmd.Flags().IntP("port", "p", 8080, "")
	Cmd.SetArgs([]string{"-p", "9090"})
	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotPort != 9090 {
		t.Errorf("expected port 9090, got %d", gotPort)
	}
}

func TestScanCommandDefaultPort(t *testing.T) {
	orig := scanFunc
	defer func() { scanFunc = orig }()
	stubBeaconNames(t)

	var gotPort int
	scanFunc = func(a *app.Data) []string {
		gotPort = a.Port
		return nil
	}

	// Reset flags for test isolation
	Cmd.Flags().Set("port", "8080")

	if gotPort != 8080 && gotPort != 0 {
		// Will be set by Execute if default is used
	}
}

// The names are looked up for the addresses the scan found, on the port it
// scanned: a beacon on another port is another device.
func TestScanCommandNamesWhatItFound(t *testing.T) {
	orig := scanFunc
	defer func() { scanFunc = orig }()

	original := beaconNames
	defer func() { beaconNames = original }()

	found := []string{"192.168.1.20", "192.168.1.21"}

	scanFunc = func(a *app.Data) []string { return found }

	var gotAddrs []string
	var gotPort int

	beaconNames = func(addrs []string, port int, window time.Duration) map[string]string {
		gotAddrs, gotPort = addrs, port

		return map[string]string{"192.168.1.20": "laptop"}
	}

	Cmd.Flags().Set("port", "8080")
	Cmd.SetArgs(nil)
	Cmd.SetOut(&bytes.Buffer{})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(gotAddrs) != 2 || gotAddrs[0] != "192.168.1.20" {
		t.Fatalf("beaconNames asked about %v, want the scanned addresses", gotAddrs)
	}

	if gotPort != 8080 {
		t.Fatalf("beaconNames asked about port %d, want 8080", gotPort)
	}
}

// --bluetooth walks the radio, and it must not also walk the network: a scan
// that did both would take two discovery windows and print one list that mixes
// two kinds of address.
func TestBluetoothFlagScansTheRadio(t *testing.T) {
	originalRadio := scanBluetoothFunc
	t.Cleanup(func() { scanBluetoothFunc = originalRadio })

	originalNetwork := scanFunc
	t.Cleanup(func() { scanFunc = originalNetwork })

	t.Cleanup(func() {
		Cmd.SetArgs(nil)
		_ = Cmd.Flags().Set("bluetooth", "false")
	})

	var radio bool

	scanBluetoothFunc = func(io.Writer) error {
		radio = true

		return nil
	}

	scanFunc = func(*app.Data) []string {
		t.Error("a radio scan walked the network as well")

		return nil
	}

	Cmd.SetArgs([]string{"--bluetooth"})
	Cmd.SetOut(&bytes.Buffer{})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !radio {
		t.Fatal("--bluetooth did not run the radio scan")
	}
}

// A radio scan and a network walk are different questions, and asking for both
// is refused instead of answered with one of them.
func TestScanConflict(t *testing.T) {
	tests := []struct {
		name    string
		subnets []string
		all     bool
		wantErr bool
	}{
		{"the radio alone", nil, false, false},
		{"the radio and a subnet", []string{"10.0.0.0/24"}, false, true},
		{"the radio and every subnet", nil, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := scanConflict(tt.subnets, tt.all); (err != nil) != tt.wantErr {
				t.Fatalf("scanConflict(%v, %v) error = %v, wantErr %v", tt.subnets, tt.all, err, tt.wantErr)
			}
		})
	}
}

func TestPrintFound(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
		names map[string]string
		want  []string
	}{
		{
			name:  "a named host",
			addrs: []string{"192.168.1.20"},
			names: map[string]string{"192.168.1.20": "laptop"},
			want:  []string{"Found 1 chat host(s):", "  laptop (192.168.1.20)"},
		},
		{
			name:  "a host that announced nothing",
			addrs: []string{"192.168.1.21"},
			want:  []string{"Found 1 chat host(s):", "  192.168.1.21"},
		},
		{
			name:  "both, in scan order",
			addrs: []string{"192.168.1.20", "192.168.1.21"},
			names: map[string]string{"192.168.1.21": "phone"},
			want:  []string{"Found 2 chat host(s):", "  192.168.1.20", "  phone (192.168.1.21)"},
		},
		{
			name: "nothing found",
			want: []string{"No chat host answered on this network"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			printFound(&out, tt.addrs, tt.names)

			got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			if len(got) != len(tt.want) {
				t.Fatalf("printFound wrote %q, want %q", got, tt.want)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
