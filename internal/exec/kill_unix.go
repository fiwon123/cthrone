//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
)

// killTree makes the timeout kill the whole process group.
//
// exec.CommandContext kills the process it started and nothing else, so a
// command that spawns a child — a daemon, a background job, a tool that shells
// out — leaves the child running after the deadline. The deadline is the promise
// that a peer cannot keep the host busy; a child that outlived it breaks that
// promise quietly.
//
// The process is put in its own group first, so the group kill cannot reach this
// program or its siblings.
func killTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}

		// The negative pid is the whole group: the command and everything it
		// started.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
