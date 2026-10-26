package recvcmd

import (
	"fmt"
	"strings"

	"github.com/fiwon123/cthrone/internal/exec"
	"github.com/spf13/cobra"
)

var allowFlag string
var logFlag string

// Cmd represents the recv command
var Cmd = &cobra.Command{
	Use:   "recv",
	Short: "opt-in remote command receiver",
	Long:  `Start an opt-in receiver that executes only allowlisted commands and logs all output.`,
	Run: func(cmd *cobra.Command, args []string) {
		var allowlist []string
		if allowFlag != "" {
			for _, a := range strings.Split(allowFlag, ",") {
				allowlist = append(allowlist, strings.TrimSpace(a))
			}
		}
		p := &exec.Policy{Allowlist: allowlist, LogFile: logFlag}
		fmt.Println("Receiver ready (opt-in). Policy loaded.")
		fmt.Printf("Allowlist: %v\n", p.Allowlist)
		if p.LogFile != "" {
			fmt.Printf("Logging to: %s\n", p.LogFile)
		}
		_ = p
	},
}

func init() {
	Cmd.Flags().StringVar(&allowFlag, "allow", "", "comma-separated allowlist (supports trailing *)")
	Cmd.Flags().StringVar(&logFlag, "log", "", "log file for audit trail")
}
