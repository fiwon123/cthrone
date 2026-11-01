package updatecmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

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
