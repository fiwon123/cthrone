package updatecmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	installpkg "github.com/fiwon123/cthrone/internal/install"
	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

// withReleaseServer points the command at a local answer for the length of one
// test. Nothing here reaches github.com: a suite that did would fail when the
// network does, and prove nothing when it passed.
func withReleaseServer(t *testing.T, status int, body string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	previousURL := releaseURL
	releaseURL = server.URL
	t.Cleanup(func() { releaseURL = previousURL })
}

// withVersion makes the command believe it is the build named.
func withVersion(t *testing.T, version string) {
	t.Helper()

	previous := Version
	Version = func() string { return version }
	t.Cleanup(func() { Version = previous })
}

// A newer release is reported with its archive, and the command succeeds: "there
// is an update" is an answer, not an error, and a script that treats a non-zero
// exit as failure must not be told the check failed.
func TestRunReportsANewerRelease(t *testing.T) {
	withVersion(t, "v0.9.0")
	withReleaseServer(t, http.StatusOK, `{"tag_name":"v0.10.0"}`)

	setCheck(t, true)

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v, want a successful check", err)
	}

	got := out.String()

	if !strings.Contains(got, "v0.10.0 is available") {
		t.Errorf("output = %q, want it to report the newer release", got)
	}

	if !strings.Contains(got, "this build is v0.9.0") {
		t.Errorf("output = %q, want it to name this build", got)
	}

	// The command line's archive, not the window's: a terminal user downloads
	// the tarball, and the two are different files.
	if want, ok := updatepkg.ArchiveName("v0.10.0", false, runtime.GOOS, runtime.GOARCH); ok && !strings.Contains(got, want) {
		t.Errorf("output = %q, want it to name %q", got, want)
	}
}

// Being up to date is an exit 0 answer, not an error: the check did its job.
func TestRunSucceedsWhenUpToDate(t *testing.T) {
	withVersion(t, "v0.10.0")
	withReleaseServer(t, http.StatusOK, `{"tag_name":"v0.10.0"}`)

	setCheck(t, true)

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v, want up-to-date to succeed", err)
	}

	if !strings.Contains(out.String(), "Up to date (v0.10.0)") {
		t.Errorf("output = %q, want it to say the build is current", out.String())
	}
}

// A check that could not be made is the one failure that returns an error, and
// the message says what went wrong rather than reporting "up to date".
func TestRunFailsWhenTheCheckCannotBeMade(t *testing.T) {
	withVersion(t, "v0.9.0")
	withReleaseServer(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)

	setCheck(t, true)

	var out bytes.Buffer

	err := run(context.Background(), &out)
	if err == nil {
		t.Fatal("run() succeeded on a 403, want an error")
	}

	if !strings.Contains(err.Error(), "could not check") {
		t.Errorf("run() error = %v, want it to say the check failed", err)
	}

	if strings.Contains(out.String(), "Up to date") {
		t.Errorf("output = %q, want no version claim on a failed check", out.String())
	}
}

// A release that names no version is a failure, not "up to date".
func TestRunFailsOnAReleaseWithNoVersion(t *testing.T) {
	withVersion(t, "v0.9.0")
	withReleaseServer(t, http.StatusOK, `{"name":"something"}`)

	setCheck(t, true)

	var out bytes.Buffer

	if err := run(context.Background(), &out); err == nil {
		t.Fatal("run() succeeded on a release with no tag, want an error")
	}
}

// Without --check the command says what it can do and makes no request: a bare
// `cthrone update` must not read as "no update available".
func TestRunWithoutCheckMakesNoRequest(t *testing.T) {
	withVersion(t, "v0.9.0")

	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requested = true
	}))
	t.Cleanup(server.Close)

	previousURL := releaseURL
	releaseURL = server.URL
	t.Cleanup(func() { releaseURL = previousURL })

	setCheck(t, false)

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v, want the hint to succeed", err)
	}

	if requested {
		t.Error("the command asked the network without --check")
	}

	if !strings.Contains(out.String(), "--check") {
		t.Errorf("output = %q, want it to name the flag that does the work", out.String())
	}
}

// A build that does not know its version is never told it is up to date: the
// vx.x.x placeholder would compare as older than everything.
func TestRunNeverCallsAnUnknownBuildCurrent(t *testing.T) {
	withVersion(t, "vx.x.x")
	withReleaseServer(t, http.StatusOK, `{"tag_name":"v0.10.0"}`)

	setCheck(t, true)

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if strings.Contains(out.String(), "Up to date") {
		t.Errorf("output = %q, want no claim about a build whose version is unknown", out.String())
	}
}

// setCheck sets the --check flag the way a command line would, and restores it.
func setCheck(t *testing.T, on bool) {
	t.Helper()

	previous := check
	check = on
	t.Cleanup(func() { check = previous })
}

// withInstall swaps the install and revert for recording fakes, and points the
// self-binary at a name the test chose.
func withInstall(t *testing.T, result installpkg.Result) *installCalls {
	t.Helper()

	calls := &installCalls{result: result}

	previousRun, previousRevert, previousSelf := runInstall, runRevert, selfBinary
	runInstall = func(_ context.Context, _ *http.Client, _ string, _ installpkg.Request) (installpkg.Result, error) {
		calls.installed++

		return result, calls.err
	}
	runRevert = func(_ string) (string, error) {
		calls.reverted++

		return result.Previous, calls.err
	}
	selfBinary = func() (string, error) { return result.Target, nil }

	t.Cleanup(func() {
		runInstall, runRevert, selfBinary = previousRun, previousRevert, previousSelf
	})

	return calls
}

type installCalls struct {
	installed int
	reverted  int
	result    installpkg.Result
	err       error
}

// --install says what it did before the user restarts: the release, the hash it
// matched, and the file the way back is in.
func TestRunInstallsARelease(t *testing.T) {
	calls := withInstall(t, installpkg.Result{
		Tag:      "v0.10.0",
		Asset:    "cthrone_v0.10.0_linux.tar.gz",
		Target:   "/usr/local/bin/cthrone",
		Previous: "/usr/local/bin/cthrone.previous",
		SHA256:   "abc123",
	})

	setAction(t, func() { doInstall = true })

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if calls.installed != 1 {
		t.Fatalf("the install ran %d times, want 1", calls.installed)
	}

	got := out.String()

	for _, want := range []string{"v0.10.0", "abc123", "cthrone.previous", "--revert"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to mention %q", got, want)
		}
	}
}

// --revert puts the previous binary back and says the way forward is the same
// command.
func TestRunReverts(t *testing.T) {
	calls := withInstall(t, installpkg.Result{
		Target:   "/usr/local/bin/cthrone",
		Previous: "/usr/local/bin/cthrone.previous",
	})

	setAction(t, func() { doRevert = true })

	var out bytes.Buffer

	if err := run(context.Background(), &out); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if calls.reverted != 1 || calls.installed != 0 {
		t.Fatalf("revert ran %d times and install %d, want 1 and 0", calls.reverted, calls.installed)
	}

	if !strings.Contains(out.String(), "previous binary") {
		t.Errorf("output = %q, want it to say the previous binary is back", out.String())
	}
}

// A failed install is a non-zero exit, and the message is what the user has to
// act on.
func TestRunReportsAFailedInstall(t *testing.T) {
	calls := withInstall(t, installpkg.Result{Target: "/x/cthrone"})
	calls.err = errors.New("the download does not match the release's checksum")

	setAction(t, func() { doInstall = true })

	var out bytes.Buffer

	err := run(context.Background(), &out)
	if err == nil {
		t.Fatal("run() succeeded on a failed install")
	}

	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("run() error = %v, want it to carry the reason", err)
	}
}

// Two actions in one command line have no order the user could have meant.
func TestRunRefusesTwoActions(t *testing.T) {
	setAction(t, func() {
		doInstall = true
		doRevert = true
	})

	if err := run(context.Background(), &bytes.Buffer{}); err == nil {
		t.Fatal("run() accepted --install and --revert together")
	}
}

// --to names a release to install, so it means nothing without --install.
func TestRunRefusesToWithoutInstall(t *testing.T) {
	setAction(t, func() { toTag = "v0.9.0" })

	if err := run(context.Background(), &bytes.Buffer{}); err == nil {
		t.Fatal("run() accepted --to without --install")
	}
}

// setAction sets the command's flags for one test and clears them afterwards.
func setAction(t *testing.T, set func()) {
	t.Helper()

	previousCheck, previousInstall, previousRevert, previousTo := check, doInstall, doRevert, toTag

	check, doInstall, doRevert, toTag = false, false, false, ""
	set()

	t.Cleanup(func() {
		check, doInstall, doRevert, toTag = previousCheck, previousInstall, previousRevert, previousTo
	})
}
