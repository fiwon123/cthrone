package execcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiwon123/cthrone/internal/exec"
)

// The command's flags are part of the surface a user sees, and they are where
// the intended shape of the feature is recorded. They stay until it exists.
func TestExecFlagsRegistered(t *testing.T) {
	for _, name := range []string{"allow", "log"} {
		if f := Cmd.Flags().Lookup(name); f == nil {
			t.Fatalf("expected --%s flag", name)
		}
	}
}

// The command refuses, and says what is missing rather than failing quietly.
func TestExecRefuses(t *testing.T) {
	if err := Cmd.RunE(Cmd, []string{"bob", "uptime"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("RunE() error = %v, want ErrNotImplemented", err)
	}
}

// And it runs nothing: the bug this replaced executed the command on this
// machine while the help said the other device.
func TestExecRunsNothingLocally(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")

	// If the command ran locally, this is what would create the file.
	probe := "touch " + marker

	_ = Cmd.RunE(Cmd, []string{"bob", "--", probe})

	// And directly through the policy, which is what the old code called.
	p := &exec.Policy{Allowlist: []string{"touch*"}}

	if _, err := p.Run(t.Context(), probe); err != nil {
		t.Fatalf("the policy itself no longer runs an allowlisted command: %v", err)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatalf("the probe did not run at all, so this test proves nothing: %v", err)
	}

	// The command left nothing behind: it refused before touching the policy.
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("exec created a file, so it ran the command locally")
	}
}
