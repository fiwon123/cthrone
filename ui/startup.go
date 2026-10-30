package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// FailureLogName is the file a GUI that could not start writes to, inside the
// user configuration directory.
//
// It exists because a GUI has nowhere else to say what went wrong: the window is
// the output, the binary is started by a click, and cthrone_gui.exe is linked
// -H=windowsgui so there is not even a console that could have shown the
// message. A failure that only returns is a failure nobody sees, and the report
// becomes "the app does not open" with nothing to act on.
const FailureLogName = "gui.log"

// logFailure records err where a user can find it and mirrors it to stderr,
// for the case where the binary was started from a terminal.
//
// It returns the file it wrote, or "" when no file could be written — a
// machine without a usable configuration directory still gets the stderr copy
// rather than nothing.
func logFailure(err error) string {
	if err == nil {
		return ""
	}

	line := fmt.Sprintf("%s  %v\n", time.Now().Format(time.RFC3339), err)

	path := failureLogPath()
	if path == "" {
		fmt.Fprint(os.Stderr, line)

		return ""
	}

	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		fmt.Fprint(os.Stderr, line)

		return ""
	}
	defer file.Close()

	fmt.Fprint(file, line)
	fmt.Fprint(os.Stderr, line)

	return path
}

// logEvent writes a diagnostic line to the failure log. It is used to
// record startup markers that are not errors themselves: a click can fail
// before the process is created, but if the process starts and dies inside
// the toolkit the log must show it began.
func logEvent(message string) string {
	if message == "" {
		return ""
	}

	line := fmt.Sprintf("%s  %s\n", time.Now().Format(time.RFC3339), message)

	path := failureLogPath()
	if path == "" {
		fmt.Fprint(os.Stderr, line)

		return ""
	}

	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		fmt.Fprint(os.Stderr, line)

		return ""
	}
	defer file.Close()

	fmt.Fprint(file, line)
	fmt.Fprint(os.Stderr, line)

	return path
}

// failureLogPath is where logFailure writes: the same configuration directory
// the CLI keeps its key and its pins in, so one machine has one place to look.
// A directory that cannot be created yields "", never an error to report — the
// caller is already handling a failure.
func failureLogPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}

	dir = filepath.Join(dir, "cthrone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}

	return filepath.Join(dir, FailureLogName)
}

// guard runs a step that must not take the window down with it, and reports
// what went wrong so the failure is recorded rather than lost.
//
// It lives here rather than beside the GUI so `make check` can prove the
// recover actually returns the panic: an empty recover() is what made a GUI
// that could not start look like a GUI that had not been clicked, and a
// recover() nobody tests can swallow the next regression just as quietly.
func guard(step string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s failed: %v\n%s", step, r, debug.Stack())
		}
	}()

	fn()

	return nil
}
