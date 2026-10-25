package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPolicyDenyByDefault(t *testing.T) {
	p := &Policy{Allowlist: []string{}, LogFile: filepath.Join(t.TempDir(), "log.txt")}
	if _, err := p.Run(context.Background(), "ls"); err == nil {
		t.Fatal("should deny")
	}
}

func TestPolicyAllowlist(t *testing.T) {
	tmp := t.TempDir()
	logf := filepath.Join(tmp, "log.txt")
	p := &Policy{Allowlist: []string{"echo hello"}, LogFile: logf, Timeout: time.Second}
	out, err := p.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected output")
	}
	if _, err := os.Stat(logf); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyWildcard(t *testing.T) {
	p := &Policy{Allowlist: []string{"echo*"}, Timeout: time.Second}
	if _, err := p.Run(context.Background(), "echo test"); err != nil {
		t.Fatal(err)
	}
}
