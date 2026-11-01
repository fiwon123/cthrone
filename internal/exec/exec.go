// Package exec runs one command a peer asked for, if the allowlist permits it.
//
// **No transport is wired to it yet.** `cthrone recv` and `cthrone exec` are
// stubs that refuse (#275); this package is the executor they will use, and its
// rules — deny by default, no shell, bounded output, the whole tree killed at
// the deadline — are written and tested ahead of the transport rather than
// alongside it.
//
// It is the most dangerous thing in this program, and the rules reflect that:
//
//   - **deny by default** — an empty allowlist allows nothing;
//   - **no shell** — the command is split into words and executed directly, so
//     a `;`, a `|` or a backtick is an argument, not a second command. There is
//     no quoting either: `"a b"` is two arguments, `"a` and `b"`, so a path with
//     a space in it cannot be passed. That is a limitation, not a rule — it is
//     the price of not having a shell to interpret;
//   - **the allowlist is a courtesy, not a sandbox** — see the note on Policy.
//
// The process is killed when the timeout expires, together with any children it
// started, and its output is bounded: a peer that asks for a command that prints
// forever must not take the host's memory with it.
package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxOutput bounds what one command may return, for the same reason the relay
// caps a file: the memory of this process belongs to the person running it, not
// to the peer that asked. Output past the cap is kept as a prefix and the rest
// is discarded as it arrives.
const maxOutput = 1 << 20

// Policy controls which commands can be executed.
//
// # The allowlist is a courtesy, not a sandbox
//
// An entry is a command string, matched exactly or as a prefix when it ends in
// `*`. That is a check on **text the peer sent**, and it is only as strong as
// the tools it names: many programs run other programs themselves — `git` with a
// `!` alias, `find` with `-exec`, `tar` with `--to-command`, a pager with `!sh`,
// an editor with `:!`. An allowlist entry for one of those is an allowlist entry
// for everything it can start.
//
// So: allowlist commands that cannot execute others, prefer exact entries to
// wildcards, and treat `recv` as "a friend may run these few commands", never as
// "this machine is safe".
type Policy struct {
	Allowlist []string
	LogFile   string
	Timeout   time.Duration
}

// timeout is the deadline for one command: the policy's, or the default.
//
// The default is not written back into the Policy: a Policy is read by every
// message the receiver handles, and a receiver that rewrote it per call would be
// a data race on a value nobody expects to change.
func (p *Policy) timeout() time.Duration {
	if p.Timeout <= 0 {
		return 30 * time.Second
	}

	return p.Timeout
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

		if strings.HasSuffix(a, "*") && strings.HasPrefix(cmd, strings.TrimSuffix(a, "*")) {
			return nil
		}
	}

	return fmt.Errorf("command not allowed: %s", cmd)
}

// Run executes a command with a timeout, without a shell, and logs its output.
//
// The returned string is what the peer gets back. It is truncated at maxOutput
// rather than grown without limit, and the truncation is marked, because a reply
// that silently stopped looks like a command that produced less than it did.
func (p *Policy) Run(ctx context.Context, cmd string) (string, error) {
	if err := p.Check(cmd); err != nil {
		return "", err
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	// #nosec G204 - the allowlist is enforced above, and there is no shell.
	proc := exec.CommandContext(c, parts[0], parts[1:]...)
	killTree(proc)

	var out boundedBuffer

	proc.Stdout = &out
	proc.Stderr = &out

	err := proc.Run()
	result := out.String()

	if c.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("command timed out after %s", p.timeout())
	}

	p.log(cmd, result)

	return result, err
}

// log appends one command and its output to the log file, when there is one.
func (p *Policy) log(cmd, result string) {
	if p.LogFile == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(p.LogFile), 0o700); err != nil {
		return
	}

	f, err := os.OpenFile(p.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "[%s] CMD: %s\n%s\n---\n", time.Now().Format(time.RFC3339), cmd, result)
}

// boundedBuffer collects output up to a limit and discards the rest.
//
// It never returns a short write to the process: a command that filled the
// buffer would otherwise see its own output refused and fail with EPIPE, which
// looks like the command broke rather than like the reply was capped.
type boundedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	room := maxOutput - b.buf.Len()
	if room > 0 {
		if len(p) < room {
			room = len(p)
		}

		b.buf.Write(p[:room])
	}

	if room < len(p) {
		b.truncated = true
	}

	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.truncated {
		return b.buf.String()
	}

	return b.buf.String() + "\n… output truncated at " + humanBytes(maxOutput)
}

var _ io.Writer = (*boundedBuffer)(nil)
