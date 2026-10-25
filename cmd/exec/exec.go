package execcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/fiwon123/cthrone/internal/exec"
	"github.com/spf13/cobra"
)

var allowFlag string
var logFlag string

// Cmd represents the exec command
var Cmd = &cobra.Command{
	Use:   "exec [device] -- [command]",
	Short: "execute a command on a remote receiver",
	Long:  `Send a command to an opt-in receiver with allowlist and logging.`,
	Run: func(cmd *cobra.Command, args []string) {
		var allowlist []string
		if allowFlag != "" {
			for _, a := range strings.Split(allowFlag, ",") {
				allowlist = append(allowlist, strings.TrimSpace(a))
			}
		}
		p := &exec.Policy{Allowlist: allowlist, LogFile: logFlag}
		if len(args) == 0 {
			fmt.Println("Usage: cthrone exec <device> -- <command>")
			return
		}
		// args[0] is device, rest would be command in real impl
		command := strings.Join(args, " ")
		out, err := p.Run(context.Background(), command)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Print(out)
	},
}

func init() {
	Cmd.Flags().StringVar(&allowFlag, "allow", "", "comma-separated allowlist (supports trailing *)")
	Cmd.Flags().StringVar(&logFlag, "log", "", "log file for audit trail")
}
