package scancmd

import (
	"fmt"
	"io"

	core "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var scanFunc = core.Scan

// beaconNames names the addresses a scan found. It is a variable so a test
// does not wait out the discovery window (or bind its port).
var beaconNames = core.BeaconNames

// Cmd represents the scan command
var Cmd = &cobra.Command{
	Use:   "scan",
	Short: "scan available IPs",
	Long: `scan available local IPs in the local subnet.

Each host is printed with the name it announces, when it announces one; a
host reachable while its broadcasts are blocked is printed by address alone.

Usage:
	cthrone scan
`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")

		data := app.New(port)
		found := scanFunc(data)

		printFound(cmd.OutOrStdout(), found, beaconNames(found, port, core.BeaconWindow))
	},
}

// printFound lists what the scan found: the address, with the device name in
// front of it where the network announced one.
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

func init() {

}
