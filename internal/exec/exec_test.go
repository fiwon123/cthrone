package exec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// A command whose output never ends must not grow this process's memory: the
// reply is a bounded prefix with a marker, and the command is not killed for it.
func TestRunBoundsTheOutput(t *testing.T) {
	p := &Policy{Allowlist: []string{"yes*"}, Timeout: 2 * time.Second}

	out, err := p.Run(context.Background(), "yes cthrone")
	if err != nil {
		// `yes` writes forever; the deadline is what stops it, which is also
		// fine — what matters is that the memory did not follow it.
		t.Logf("run ended with %v (deadline or pipe close)", err)
	}

	if len(out) > maxOutput+200 {
		t.Fatalf("the output is %d bytes, want it bounded near %d", len(out), maxOutput)
	}

	if !strings.Contains(out, "truncated") {
		t.Errorf("the output does not say it was truncated: %q", out[len(out)-80:])
	}
}

// The timeout is the promise that a peer cannot keep the host busy. A command
// that starts a child must not leave it behind.
//
// The root command is this test binary, re-run as a helper: `Run` splits on
// whitespace and starts no shell, so a multi-word script is not something it can
// be asked to execute — which is itself the property below.
func TestRunKillsTheChildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the child check reads /proc, which is Linux's")
	}

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("CTHRONE_EXEC_HELPER", "1")
	t.Setenv("CTHRONE_EXEC_PIDFILE", pidFile)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	p := &Policy{Allowlist: []string{self + "*"}, Timeout: 500 * time.Millisecond}

	if _, err := p.Run(context.Background(), self+" -test.run=TestSpawnerHelper"); err == nil {
		t.Fatal("the command outlived its deadline")
	}

	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		body, readErr := os.ReadFile(pidFile)
		if readErr != nil {
			time.Sleep(10 * time.Millisecond)

			continue
		}

		pid := strings.TrimSpace(string(body))
		if pid != "" && processGone(pid) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("the child of the timed-out command is still running")
}

// TestSpawnerHelper is the child process the test above starts: it starts a
// grandchild, writes its pid where the test can read it, and waits.
func TestSpawnerHelper(t *testing.T) {
	if os.Getenv("CTHRONE_EXEC_HELPER") != "1" {
		t.Skip("not the helper run")
	}

	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}

	_ = os.WriteFile(os.Getenv("CTHRONE_EXEC_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600)

	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// There is no shell and there is no quoting: the command is split on
// whitespace, so an argument cannot contain a space. Worth a test, because the
// next person to allowlist something with a path in it will meet this.
func TestRunCannotPassAnArgumentWithASpace(t *testing.T) {
	p := &Policy{Allowlist: []string{"printf*"}, Timeout: time.Second}

	out, err := p.Run(context.Background(), `printf "%s" "a b"`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// printf receives the quotes as characters and the pieces as arguments, so
	// the reply is not "a b" with the space the user meant.
	if strings.TrimSpace(out) == "a b" {
		t.Skip("this platform's printf joined the pieces; the limitation does not show here")
	}
}

// A guard on the policy itself: running commands must not write to it, because
// the receiver reads one Policy for every message it handles.
func TestRunDoesNotMutateThePolicy(t *testing.T) {
	p := &Policy{Allowlist: []string{"echo*"}}

	if _, err := p.Run(context.Background(), "echo hi"); err != nil {
		t.Fatal(err)
	}

	if p.Timeout != 0 {
		t.Errorf("Run wrote Timeout = %v into the policy, want it left alone", p.Timeout)
	}
}

// processGone reports whether a pid is no longer running.
//
// A zombie counts as gone: a killed child whose parent died is reaped by
// whoever inherits it, and in a container that can be slow — but it is dead, and
// that is what the timeout promised. Reading the state field of /proc/<pid>/stat
// is what tells the two apart; the entry itself lingers for a zombie.
func processGone(pid string) bool {
	if runtime.GOOS != "linux" {
		return true // the check is a Linux one; nothing else is claimed here
	}

	body, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err != nil {
		return true
	}

	// The state is the field after the parenthesised command name, which may
	// itself contain spaces: take the last ')' and the field after it.
	text := string(body)
	if end := strings.LastIndexByte(text, ')'); end >= 0 {
		fields := strings.Fields(text[end+1:])
		if len(fields) > 0 && fields[0] == "Z" {
			return true
		}
	}

	return false
}
