// Package recvcmd is `cthrone recv`, the command that was meant to be the
// receiver for `cthrone exec`.
//
// It is not implemented. It used to print "Receiver ready (opt-in). Policy
// loaded." and return, which told a user a receiver was listening when nothing
// was: the one thing this command must never do is imply a security feature is
// running. It refuses, by name, until the transport that carries a command
// exists (#275).
package recvcmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var allowFlag string
var logFlag string

// ErrNotImplemented is what the command answers until it exists.
var ErrNotImplemented = errors.New("recv: remote command execution is not implemented yet (#275) — nothing is listening, and no command sent by a peer would run")

// Cmd represents the recv command.
var Cmd = &cobra.Command{
	Use:   "recv",
	Short: "opt-in remote command receiver (not implemented yet)",
	Long: `Start an opt-in receiver that executes only allowlisted commands and logs all
output.

NOT IMPLEMENTED. The allowlist and the audit log exist (internal/exec) and are
tested, but no transport carries a command to this process yet, so this command
refuses rather than pretending to listen. See issue #275.`,
	SilenceUsage: true,
	RunE: func(*cobra.Command, []string) error {
		return ErrNotImplemented
	},
}

func init() {
	Cmd.Flags().StringVar(&allowFlag, "allow", "", "comma-separated allowlist (supports trailing *)")
	Cmd.Flags().StringVar(&logFlag, "log", "", "log file for audit trail")
}
