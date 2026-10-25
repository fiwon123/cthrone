package exec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Policy controls which commands can be executed.
type Policy struct {
	Allowlist []string
	LogFile   string
	Timeout   time.Duration
}

// Check returns error if command not allowed.
func (p *Policy) Check(cmd string) error {
	if len(p.Allowlist) == 0 {
		return fmt.Errorf("no commands allowed")
	}
	for _, a := range p.Allowlist {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if a == cmd {
			return nil
		}
		if strings.HasSuffix(a, "*") {
			prefix := strings.TrimSuffix(a, "*")
			if strings.HasPrefix(cmd, prefix) {
				return nil
			}
		}
	}
	return fmt.Errorf("command not allowed: %s", cmd)
}

// Run executes a command with timeout, no shell by default, logs output.
func (p *Policy) Run(ctx context.Context, cmd string) (string, error) {
	if err := p.Check(cmd); err != nil {
		return "", err
	}
	if p.Timeout == 0 {
		p.Timeout = 30 * time.Second
	}
	c, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}
	name := parts[0]
	args := parts[1:]
	// #nosec G204 - allowlist enforced
	proc := exec.CommandContext(c, name, args...)
	out, err := proc.CombinedOutput()
	result := string(out)
	if p.LogFile != "" {
		_ = os.MkdirAll(filepath.Dir(p.LogFile), 0700)
		f, ferr := os.OpenFile(p.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if ferr == nil {
			ts := time.Now().Format(time.RFC3339)
			fmt.Fprintf(f, "[%s] CMD: %s\n%s\n---\n", ts, cmd, result)
			f.Close()
		}
	}
	if err != nil {
		return result, err
	}
	return result, nil
}
