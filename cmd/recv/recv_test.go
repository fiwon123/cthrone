package recvcmd

import (
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/exec"
)

func TestRecvFlagsRegistered(t *testing.T) {
	if f := Cmd.Flags().Lookup("allow"); f == nil {
		t.Fatal("expected --allow flag")
	}
	if f := Cmd.Flags().Lookup("log"); f == nil {
		t.Fatal("expected --log flag")
	}
}

func TestRecvAllowlistParsing(t *testing.T) {
	allowFlag = " echo hello , ls* ,  "
	logFlag = "/tmp/test.log"
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
	if p.Allowlist[0] != "echo hello" || p.Allowlist[1] != "ls*" {
		t.Fatalf("unexpected allowlist: %v", p.Allowlist)
	}
	if p.LogFile != "/tmp/test.log" {
		t.Fatalf("log file = %s", p.LogFile)
	}
}
