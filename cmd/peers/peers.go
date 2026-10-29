// Package peerscmd implements the `peers` command: the way to see and forget
// the devices this one has pinned on first contact.
package peerscmd

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/spf13/cobra"
)

var peersFileFlag string

// Cmd represents the peers command
var Cmd = &cobra.Command{
	Use:   "peers [forget <name|fingerprint>]",
	Short: "list the devices this one has paired with, or forget one",
	Long: `list the devices this one has paired with, or forget one.

A device is pinned the first time it completes the handshake: its name and key
are recorded, and every later contact has to present the same key. A peer that
comes back with a different key is refused rather than re-pinned.

Forgetting is the way to re-pair a device that reinstalled the app, or to undo
a pairing that was made by mistake. It is also the way to revoke a peer this
one no longer trusts.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := pinFile(peersFileFlag)
		if err != nil {
			return err
		}

		return run(args, path, cmd.OutOrStdout())
	},
}

func init() {
	Cmd.Flags().StringVar(&peersFileFlag, "peers-file", "", "the file the pins are read from (default: the user configuration directory)")
}

// run is the command without cobra around it, so a test can drive it.
//
// An argument is an action: no argument lists, "forget" takes one name or
// fingerprint. An action that does not name a pin is an error rather than a
// silent success, because a forget that did nothing looks exactly like one
// that worked.
func run(args []string, path string, out io.Writer) error {
	store, err := discovery.NewPinStore(path)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return list(store, out)
	}

	if args[0] != "forget" {
		return fmt.Errorf("unknown action %q: `peers` lists, `peers forget <name|fingerprint>` forgets", args[0])
	}

	if len(args) != 2 {
		return errors.New("forget needs one name or fingerprint")
	}

	forgot, err := store.Forget(args[1])
	if err != nil {
		return err
	}

	if !forgot {
		return fmt.Errorf("no pin matches %q", args[1])
	}

	fmt.Fprintln(out, "Forgot", args[1])

	return nil
}

// list prints the pinned devices. The two times are the answer to "when did I
// pair with this, and when did it last talk to me", which is what tells a
// forgotten device from one that is simply quiet.
func list(store *discovery.PinStore, out io.Writer) error {
	peers := store.List()
	if len(peers) == 0 {
		fmt.Fprintln(out, "No pinned devices yet: a device is pinned the first time it completes the handshake.")

		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "NAME\tFINGERPRINT\tFIRST SEEN\tLAST SEEN")

	for _, p := range peers {
		name := p.Name
		if name == "" {
			name = "(unnamed)"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, p.Fingerprint, stamp(p.FirstSeen), stamp(p.LastSeen))
	}

	return w.Flush()
}

// pinFile is the file the pins are kept in.
func pinFile(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return discovery.DefaultPinsFile()
}

// stamp is a time in the form a terminal table can hold.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	return t.Format(time.RFC3339)
}
