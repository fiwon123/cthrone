package scancmd

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagCmd is a command carrying only the scan flags, so a test of the flag
// parsing does not inherit whatever the previous test left on the real one.
func flagCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	c := &cobra.Command{}
	c.Flags().StringArray("subnet", nil, "")
	c.Flags().Bool("all-subnets", false, "")

	if err := c.Flags().Parse(args); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}

	return c
}

func stubLocalSubnets(t *testing.T, prefixes []netip.Prefix) {
	t.Helper()

	orig := localSubnetsFunc
	t.Cleanup(func() { localSubnetsFunc = orig })

	localSubnetsFunc = func() []netip.Prefix { return prefixes }
}

func TestTargetsFromFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		local   []netip.Prefix
		want    []string
		wantErr bool
	}{
		{
			name: "no flag walks nothing explicit",
		},
		{
			name: "one subnet",
			args: []string{"--subnet", "192.168.1.0/24"},
			want: []string{"192.168.1.0/24"},
		},
		{
			name: "the flag is repeatable",
			args: []string{"--subnet", "192.168.1.0/24", "--subnet", "10.0.0.0/24"},
			want: []string{"192.168.1.0/24", "10.0.0.0/24"},
		},
		{
			name:  "every local interface",
			args:  []string{"--all-subnets"},
			local: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24"), netip.MustParsePrefix("10.1.2.0/24")},
			want:  []string{"192.168.0.0/24", "10.1.2.0/24"},
		},
		{
			name:  "a local subnet and a named one together",
			args:  []string{"--all-subnets", "--subnet", "192.168.1.0/24"},
			local: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24")},
			want:  []string{"192.168.0.0/24", "192.168.1.0/24"},
		},
		{
			name:  "naming the interface's own subnet twice walks it once",
			args:  []string{"--all-subnets", "--subnet", "192.168.0.0/24"},
			local: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24")},
			want:  []string{"192.168.0.0/24"},
		},
		{
			name:    "a value that is not a CIDR is refused",
			args:    []string{"--subnet", "192.168.1.5"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLocalSubnets(t, tt.local)

			got, err := targetsFromFlags(flagCmd(t, tt.args...))
			if (err != nil) != tt.wantErr {
				t.Fatalf("targetsFromFlags(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("targetsFromFlags(%v) = %v, want %v", tt.args, got, tt.want)
			}

			for i := range tt.want {
				if got[i].String() != tt.want[i] {
					t.Errorf("target %d = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPrintTargets(t *testing.T) {
	targets := []netip.Prefix{
		netip.MustParsePrefix("192.168.1.0/24"),
		netip.MustParsePrefix("10.0.0.0/24"),
	}

	var out bytes.Buffer

	printTargets(&out, targets, []string{"192.168.1.20"}, map[string]string{"192.168.1.20": "laptop"})

	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")

	want := []string{
		"192.168.1.0/24: 1 chat host(s)",
		"  laptop (192.168.1.20)",
		"10.0.0.0/24: no chat host answered",
		"  a segment this device cannot route to looks the same — NAT or client isolation on the router cannot be crossed by a scan",
	}

	if len(got) != len(want) {
		t.Fatalf("printTargets wrote %q, want %q", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A found address that belongs to no requested subnet must not be printed under
// one of them.
func TestPrintTargetsOnlyListsAddressesInsideTheSubnet(t *testing.T) {
	var out bytes.Buffer

	printTargets(&out,
		[]netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
		[]string{"192.168.1.20", "10.9.9.9"},
		nil)

	text := out.String()

	if strings.Contains(text, "10.9.9.9") {
		t.Fatalf("printTargets printed an address outside the subnet: %q", text)
	}

	if !strings.Contains(text, "192.168.1.20") {
		t.Fatalf("printTargets dropped the address inside the subnet: %q", text)
	}
}

// resetScanFlags puts the real command's flags back, so a test that executes it
// does not leave --subnet behind for the next one.
func resetScanFlags(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		if value, ok := Cmd.Flags().Lookup("subnet").Value.(pflag.SliceValue); ok {
			_ = value.Replace(nil)
		}

		_ = Cmd.Flags().Set("all-subnets", "false")
		Cmd.SetArgs(nil)
	})
}

func TestScanCommandWalksTheRequestedSubnet(t *testing.T) {
	origScan, origSubnets := scanFunc, scanSubnetsFunc
	origNames := beaconNames

	t.Cleanup(func() {
		scanFunc, scanSubnetsFunc, beaconNames = origScan, origSubnets, origNames
	})

	resetScanFlags(t)
	stubBeaconNames(t)

	var (
		gotTargets []netip.Prefix
		gotTimeout time.Duration
	)

	scanSubnetsFunc = func(_ *app.Data, targets []netip.Prefix, timeout time.Duration) ([]string, error) {
		gotTargets, gotTimeout = targets, timeout

		return []string{"192.168.1.20"}, nil
	}

	// The default walk must not run: the user asked for a specific subnet.
	scanFunc = func(*app.Data) []string {
		t.Fatal("the default scan ran even though --subnet was given")

		return nil
	}

	var out bytes.Buffer

	Cmd.SetOut(&out)
	Cmd.SetArgs([]string{"--subnet", "192.168.1.0/24"})

	if err := Cmd.Execute(); err != nil {
		t.Fatalf("Execute error = %v", err)
	}

	if len(gotTargets) != 1 || gotTargets[0].String() != "192.168.1.0/24" {
		t.Fatalf("scanSubnets asked for %v, want 192.168.1.0/24", gotTargets)
	}

	if gotTimeout != 0 {
		t.Errorf("timeout = %v, want 0 so the scanner applies its own default", gotTimeout)
	}

	if !strings.Contains(out.String(), "192.168.1.0/24: 1 chat host(s)") {
		t.Fatalf("output %q does not report the subnet it walked", out.String())
	}
}

func TestScanCommandRefusesAnInvalidSubnet(t *testing.T) {
	origSubnets := scanSubnetsFunc
	t.Cleanup(func() { scanSubnetsFunc = origSubnets })

	resetScanFlags(t)

	scanSubnetsFunc = func(*app.Data, []netip.Prefix, time.Duration) ([]string, error) {
		t.Fatal("a scan ran with an invalid subnet")

		return nil, nil
	}

	Cmd.SetOut(&bytes.Buffer{})
	Cmd.SetArgs([]string{"--subnet", "not-a-subnet"})

	err := Cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded with an invalid --subnet")
	}

	if !strings.Contains(err.Error(), "not-a-subnet") {
		t.Fatalf("error %q does not name the bad value", err)
	}
}
