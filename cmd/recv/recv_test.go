package recvcmd

import (
	"errors"
	"strings"
	"testing"
)

func TestRecvFlagsRegistered(t *testing.T) {
	for _, name := range []string{"allow", "log"} {
		if f := Cmd.Flags().Lookup(name); f == nil {
			t.Fatalf("expected --%s flag", name)
		}
	}
}

// The receiver does not exist yet, and the one thing it must never do is claim
// to be listening when it is not — which is what it used to print.
func TestRecvRefuses(t *testing.T) {
	err := Cmd.RunE(Cmd, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("RunE() error = %v, want ErrNotImplemented", err)
	}

	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("the refusal does not say it is unimplemented: %q", err.Error())
	}

	if strings.Contains(err.Error(), "ready") {
		t.Errorf("the refusal claims the receiver is ready: %q", err.Error())
	}
}
