package ui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A GUI that fails to start must leave a record. This is the difference between
// "the app does not open" and a bug report with a stack trace in it.
func TestLogFailureWritesWhereItCanBeFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := logFailure(errors.New("the GUI stopped: boom"))

	want := filepath.Join(dir, "cthrone", FailureLogName)
	if path != want {
		t.Fatalf("logFailure wrote to %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if !strings.Contains(string(data), "boom") {
		t.Errorf("the log does not carry the failure: %q", data)
	}
}

// The log accumulates: one entry per failure, so a user who starts the app
// twice before reporting gets both attempts.
func TestLogFailureAppends(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path := logFailure(errors.New("first"))
	logFailure(errors.New("second"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	for _, want := range []string{"first", "second"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the log does not carry %q: %q", want, data)
		}
	}
}

// The log is not world-readable: it can carry a file path and a stack trace,
// and neither belongs to every user on the machine.
func TestFailureLogIsNotReadableByOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mode bits are not the Windows ACL")
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	info, err := os.Stat(logFailure(errors.New("boom")))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("gui.log mode = %o, want 600", mode)
	}
}

// A configuration directory that cannot be created must not turn one failure
// into another: the message still goes to stderr, and the caller is told there
// is no file to point at.
func TestLogFailureWithoutAUsableConfigDir(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("write %s: %v", blocked, err)
	}

	t.Setenv("XDG_CONFIG_HOME", blocked)

	if path := logFailure(errors.New("boom")); path != "" {
		t.Errorf("logFailure reported a file %q although the directory is a file", path)
	}
}

// Nothing to report is not a failure, and must not create a log file.
func TestLogFailureIgnoresNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if path := logFailure(nil); path != "" {
		t.Errorf("logFailure(nil) = %q, want no file", path)
	}

	if _, err := os.Stat(filepath.Join(dir, "cthrone", FailureLogName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("logFailure(nil) touched the log: %v", err)
	}
}

// A step that panics is reported, not swallowed. The empty recover() this
// replaces is what turned "no OpenGL", "no config directory" and every other
// startup failure into a window that never appeared and an exit code of 0.
func TestGuardReportsAPanic(t *testing.T) {
	err := guard("discovery", func() { panic("boom") })

	if err == nil {
		t.Fatal("guard swallowed the panic")
	}

	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("guard error does not name the panic: %v", err)
	}

	if !strings.Contains(err.Error(), "discovery") {
		t.Errorf("guard error does not name the step that failed: %v", err)
	}

	if !strings.Contains(err.Error(), "goroutine") {
		t.Errorf("guard error carries no stack: %v", err)
	}
}

// A step that succeeds reports nothing.
func TestGuardPassesASuccessfulStep(t *testing.T) {
	ran := false

	if err := guard("resize", func() { ran = true }); err != nil {
		t.Fatalf("guard = %v, want nil", err)
	}

	if !ran {
		t.Error("guard did not run the step")
	}
}
