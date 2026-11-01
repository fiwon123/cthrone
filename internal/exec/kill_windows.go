//go:build windows

package exec

import (
	"os/exec"
	"strconv"
)

// killTree makes the timeout kill the process and its children.
//
// Windows has no process-group kill through os/exec, so the reliable way to take
// a tree down is taskkill with /T. It is one more program started by this
// program, which is worth saying out loud: the alternative — killing only the
// parent and leaving a child holding a file — is the worse surprise.
func killTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}

		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}

		return nil
	}
}
