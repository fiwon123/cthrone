// Package execcmd is `cthrone exec`, the command that was meant to send one
// commanded line to a peer running `cthrone recv`.
//
// It is not implemented, and it used to be worse than absent: it joined the
// device name and the command into one string and ran it **on this machine**
// against a local allowlist, while its help said "execute a command on a remote
// receiver". A user who believed the help was running a command here.
//
// It refuses, by name, and runs nothing (#275).
package execcmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var allowFlag string
var logFlag string

// ErrNotImplemented is what the command answers until it exists.
var ErrNotImplemented = errors.New("exec: running a command on a remote device is not implemented yet (#275) — nothing was sent and nothing was run")

// Cmd represents the exec command.
var Cmd = &cobra.Command{
	Use:   "exec [device] -- [command]",
	Short: "execute a command on a remote receiver (not implemented yet)",
	Long: `Send a command to an opt-in receiver with allowlist and logging.

NOT IMPLEMENTED. Nothing is sent to the device and nothing is executed here
either: an earlier version ran the command locally while this help said it was
remote. See issue #275.`,
	SilenceUsage: true,
	RunE: func(*cobra.Command, []string) error {
		return ErrNotImplemented
	},
}

func init() {
	Cmd.Flags().StringVar(&allowFlag, "allow", "", "comma-separated allowlist (supports trailing *)")
	Cmd.Flags().StringVar(&logFlag, "log", "", "log file for audit trail")
}
