package scancmd

import (
	"fmt"
	"io"
	"net/netip"

	core "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var scanFunc = core.Scan

// scanSubnetsFunc walks the subnets named on the command line. It is a
// variable so a test does not dial anything.
var scanSubnetsFunc = core.ScanSubnets

// localSubnetsFunc is the /24 of every local interface, for --all-subnets.
var localSubnetsFunc = core.LocalSubnets

// beaconNames names the addresses a scan found. It is a variable so a test
// does not wait out the discovery window (or bind its port).
var beaconNames = core.BeaconNames

// Cmd represents the scan command
var Cmd = &cobra.Command{
	Use:   "scan",
	Short: "scan available IPs",
	Long: `scan available local IPs.

With no flag the scan walks the /24 this device is on — the subnet the default
route uses.

--subnet walks another segment as well, which is how a device on a second
Wi-Fi, a VLAN or a wired segment is found when the router routes between them.
The flag is repeatable. --all-subnets walks the /24 of every local interface.

A scan only reaches what this device can route to. A segment behind a NAT, or
one whose router isolates its clients, answers nothing and looks the same as an
empty segment; the output says so, because the two have different fixes.

Each host is printed with the name it announces, when it announces one. The
name comes from a broadcast that does not cross a subnet, so a host on another
segment is printed by address alone.

Usage:
	cthrone scan
	cthrone scan --subnet 192.168.1.0/24
	cthrone scan --subnet 192.168.1.0/24 --subnet 10.0.0.0/24
	cthrone scan --all-subnets
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")

		data := app.New(port)

		targets, err := targetsFromFlags(cmd)
		if err != nil {
			return err
		}

		// No subnet named: the scan is exactly what it always was, including
		// its output.
		if len(targets) == 0 {
			found := scanFunc(data)
			printFound(cmd.OutOrStdout(), found, beaconNames(found, port, core.BeaconWindow))

			return nil
		}

		found, err := scanSubnetsFunc(data, targets, 0)
		if err != nil {
			return err
		}

		printTargets(cmd.OutOrStdout(), targets, found, beaconNames(found, port, core.BeaconWindow))

		return nil
	},
}

// targetsFromFlags collects the subnets the user asked for: every local
// interface with --all-subnets, plus each --subnet. Duplicates are dropped, so
// naming the interface's own subnet twice walks it once.
func targetsFromFlags(cmd *cobra.Command) ([]netip.Prefix, error) {
	values, _ := cmd.Flags().GetStringArray("subnet")
	all, _ := cmd.Flags().GetBool("all-subnets")

	var targets []netip.Prefix

	if all {
		targets = append(targets, localSubnetsFunc()...)
	}

	explicit, err := core.ParseSubnets(values)
	if err != nil {
		return nil, err
	}

	return dedupe(append(targets, explicit...)), nil
}

// dedupe drops repeated prefixes, keeping the first occurrence so the order the
// user wrote is the order that is printed.
func dedupe(prefixes []netip.Prefix) []netip.Prefix {
	seen := make(map[netip.Prefix]bool, len(prefixes))
	out := make([]netip.Prefix, 0, len(prefixes))

	for _, p := range prefixes {
		if seen[p] {
			continue
		}

		seen[p] = true

		out = append(out, p)
	}

	return out
}

// printFound lists what the default scan found: the address, with the device
// name in front of it where the network announced one.
func printFound(out io.Writer, addrs []string, names map[string]string) {
	if len(addrs) == 0 {
		fmt.Fprintln(out, "No chat host answered on this network")

		return
	}

	fmt.Fprintf(out, "Found %d chat host(s):\n", len(addrs))

	for _, addr := range addrs {
		if name := names[addr]; name != "" {
			fmt.Fprintf(out, "  %s (%s)\n", name, addr)

			continue
		}

		fmt.Fprintf(out, "  %s\n", addr)
	}
}

// printTargets reports each subnet the user asked for separately, so "nothing
// answered here" is attached to the segment it is about.
//
// A subnet with no answer gets the sentence that explains it: a scan cannot
// tell an empty segment from one it cannot reach, and the difference is the
// whole point of asking for a second subnet.
func printTargets(out io.Writer, targets []netip.Prefix, found []string, names map[string]string) {
	for _, target := range targets {
		var in []string

		for _, addr := range found {
			ip, err := netip.ParseAddr(addr)
			if err != nil || !target.Contains(ip) {
				continue
			}

			in = append(in, addr)
		}

		if len(in) == 0 {
			fmt.Fprintf(out, "%s: no chat host answered\n", target)
			fmt.Fprintf(out, "  a segment this device cannot route to looks the same — NAT or client isolation on the router cannot be crossed by a scan\n")

			continue
		}

		fmt.Fprintf(out, "%s: %d chat host(s)\n", target, len(in))

		for _, addr := range in {
			if name := names[addr]; name != "" {
				fmt.Fprintf(out, "  %s (%s)\n", name, addr)

				continue
			}

			fmt.Fprintf(out, "  %s\n", addr)
		}
	}
}

func init() {
	Cmd.Flags().StringArray("subnet", nil, "an IPv4 CIDR to scan as well, e.g. 192.168.1.0/24; repeat for several segments")
	Cmd.Flags().Bool("all-subnets", false, "scan the /24 of every local network interface")
}
