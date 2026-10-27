package execcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/exec"
)

func TestExecFlagsRegistered(t *testing.T) {
	if f := Cmd.Flags().Lookup("allow"); f == nil {
		t.Fatal("expected --allow flag")
	}
	if f := Cmd.Flags().Lookup("log"); f == nil {
		t.Fatal("expected --log flag")
	}
}

func TestExecAllowlistParsing(t *testing.T) {
	allowFlag = "echo hello,uptime*"
	logFlag = ""
	defer func() { allowFlag = ""; logFlag = "" }()

	var allowlist []string
	for _, a := range strings.Split(allowFlag, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		allowlist = append(allowlist, a)
	}
	p := &exec.Policy{Allowlist: allowlist, LogFile: logFlag}
	if len(p.Allowlist) != 2 {
		t.Fatalf("expected 2 entries, got %v", p.Allowlist)
	}
	if p.Allowlist[0] != "echo hello" || p.Allowlist[1] != "uptime*" {
		t.Fatalf("unexpected allowlist: %v", p.Allowlist)
	}
}

func TestExecRunWithPolicy(t *testing.T) {
	p := &exec.Policy{Allowlist: []string{"echo hello"}}
	out, err := p.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected output")
	}
}
