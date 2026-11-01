//go:build gui

package ui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	installpkg "github.com/fiwon123/cthrone/internal/install"
)

// installRecorder stands in for the install path: it records the request and
// answers with what the test told it to.
type installRecorder struct {
	mu       sync.Mutex
	requests []installpkg.Request
	reverts  []string
	result   installpkg.Result
	err      error
}

func (r *installRecorder) install(_ context.Context, _ *http.Client, _ string, request installpkg.Request) (installpkg.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests = append(r.requests, request)

	return r.result, r.err
}

func (r *installRecorder) revert(target string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reverts = append(r.reverts, target)

	return r.result.Previous, r.err
}

func (r *installRecorder) installs() []installpkg.Request {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]installpkg.Request(nil), r.requests...)
}

func (r *installRecorder) revertCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.reverts)
}

// withInstallRecorder swaps the install path for the recorder, and the target
// for a file in a temporary directory — so a test never writes over the binary
// it is running from.
func withInstallRecorder(t *testing.T) (*installRecorder, string) {
	t.Helper()

	recorder := &installRecorder{
		result: installpkg.Result{Tag: "v0.10.0", Target: "", Previous: ""},
	}

	target := filepath.Join(t.TempDir(), "cthrone")
	if err := os.WriteFile(target, []byte("the test binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	previousRun, previousRevert, previousSelf := runInstall, runRevert, selfBinary
	runInstall = recorder.install
	runRevert = recorder.revert
	selfBinary = func() (string, error) { return target, nil }

	t.Cleanup(func() { runInstall, runRevert, selfBinary = previousRun, previousRevert, previousSelf })

	return recorder, target
}

// acceptConfirm makes the question answer itself, so a test can drive the path
// after the user said yes.
func acceptConfirm(t *testing.T) {
	t.Helper()

	previous := askConfirm
	askConfirm = func(_ fyne.Window, _, _ string, onYes func()) { onYes() }

	t.Cleanup(func() { askConfirm = previous })
}

// The Install button is offered for a release that is newer than this build,
// and not for one that is not — nor for a build that cannot read its own
// version, where the comparison would be a guess.
func TestInstallButtonAppearsOnlyForANewerRelease(t *testing.T) {
	g := newScreenGUI(t, false)

	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"a newer release", "v0.9.0", "v0.10.0", true},
		{"the same release", "v0.10.0", "v0.10.0", false},
		{"an older release published last", "v0.11.0", "v0.10.0", false},
		{"a build with no readable version", placeholderVersion, "v0.10.0", false},
		{"a release with no readable version", "v0.9.0", "nightly", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.offerNewer(tt.current, tt.latest)

			if got := g.installBtn.Visible(); got != tt.want {
				t.Fatalf("the Install button is visible = %v, want %v", got, tt.want)
			}

			if tt.want && !strings.Contains(g.installBtn.Text(), tt.latest) {
				t.Errorf("the button reads %q, want it to name %q", g.installBtn.Text(), tt.latest)
			}
		})
	}
}

// Nothing is replaced before the user answers: pressing Install opens the
// question and stops there.
func TestInstallAsksBeforeReplacing(t *testing.T) {
	recorder, _ := withInstallRecorder(t)

	g := newScreenGUI(t, false)
	g.offerNewer("v0.9.0", "v0.10.0")

	g.installRelease()

	// The install runs off the UI goroutine, so give one the chance to start:
	// an install launched without waiting for the answer would land here.
	time.Sleep(50 * time.Millisecond)

	if len(recorder.installs()) != 0 {
		t.Fatalf("%d installs ran before the question was answered", len(recorder.installs()))
	}

	if g.win.Canvas().Overlays().Top() == nil {
		t.Error("pressing Install showed no confirmation")
	}
}

// Answering yes installs the release the check named, as the window build, at
// the binary that is running — and says so where the user is looking.
func TestInstallRunsWhenConfirmed(t *testing.T) {
	recorder, target := withInstallRecorder(t)
	recorder.result = installpkg.Result{
		Tag:      "v0.10.0",
		Target:   target,
		Previous: target + installpkg.PreviousSuffix,
	}

	acceptConfirm(t)

	g := newScreenGUI(t, false)
	g.offerNewer("v0.9.0", "v0.10.0")

	g.runInstall("v0.10.0")
	waitForAboutNote(t, g, "Installed v0.10.0")

	requests := recorder.installs()
	if len(requests) != 1 {
		t.Fatalf("%d installs ran, want 1", len(requests))
	}

	request := requests[0]

	if request.Tag != "v0.10.0" {
		t.Errorf("the install asked for %q, want the release the check named", request.Tag)
	}

	if !request.GUIBuild {
		t.Error("the install asked for the command line build, want the window's")
	}

	if request.Target != target {
		t.Errorf("the install targeted %q, want the running binary %q", request.Target, target)
	}
}

// A refusal is reported on the tab, and the button comes back so the user can
// try again or do something else.
func TestInstallFailureIsReported(t *testing.T) {
	recorder, _ := withInstallRecorder(t)
	recorder.err = installpkg.ErrChecksum

	g := newScreenGUI(t, false)
	g.offerNewer("v0.9.0", "v0.10.0")

	g.runInstall("v0.10.0")

	note := waitForAboutNote(t, g, "Could not install")

	if !strings.Contains(note, "checksum") {
		t.Errorf("the note = %q, want it to carry the reason", note)
	}

	if g.installBtn.Disabled() {
		t.Error("the Install button stayed disabled after a failure")
	}
}

// The way back is offered only when there is one: a button that can only answer
// "no previous binary" reads as a broken feature.
func TestRevertButtonFollowsTheDisk(t *testing.T) {
	_, target := withInstallRecorder(t)

	g := newScreenGUI(t, false)

	g.refreshRevert()
	if g.revertBtn.Visible() {
		t.Error("the Revert button is visible with no previous binary beside this one")
	}

	if err := os.WriteFile(target+installpkg.PreviousSuffix, []byte("older"), 0o755); err != nil {
		t.Fatal(err)
	}

	g.refreshRevert()
	if !g.revertBtn.Visible() {
		t.Error("the Revert button is hidden although a previous binary is there")
	}

	if err := os.Remove(target + installpkg.PreviousSuffix); err != nil {
		t.Fatal(err)
	}

	g.refreshRevert()
	if g.revertBtn.Visible() {
		t.Error("the Revert button is still visible after the previous binary went away")
	}
}

// Revert needs no network: it asks, then puts the binary that is already beside
// this one back.
func TestRevertRunsWhenConfirmed(t *testing.T) {
	recorder, _ := withInstallRecorder(t)
	acceptConfirm(t)

	g := newScreenGUI(t, false)
	g.revertRelease()

	waitForAboutNote(t, g, "previous binary")

	if recorder.revertCount() != 1 {
		t.Fatalf("%d reverts ran, want 1", recorder.revertCount())
	}
}

// A revert that fails says why rather than looking like it worked.
func TestRevertFailureIsReported(t *testing.T) {
	recorder, _ := withInstallRecorder(t)
	recorder.err = errors.New("no previous binary to revert to")

	g := newScreenGUI(t, false)

	g.runRevert()

	note := waitForAboutNote(t, g, "Could not go back")

	if !strings.Contains(note, "no previous binary") {
		t.Errorf("the note = %q, want it to carry the reason", note)
	}
}

// The About tab links the walk-through, because the README is the release
// claim and the wiki is the page a user at "how do I host?" actually wants.
func TestAboutWikiButtonOpensTheWiki(t *testing.T) {
	g := newScreenGUI(t, false)
	recorder := withOpener(t)

	if g.wikiBtn == nil {
		t.Fatal("the About tab has no wiki button")
	}

	test.Tap(g.wikiBtn)

	if len(recorder.opened) != 1 {
		t.Fatalf("the wiki button opened %v, want one address", recorder.opened)
	}

	if got, want := recorder.opened[0], WikiURL; got != want {
		t.Errorf("the wiki button opened %q, want %q", got, want)
	}

	if !strings.HasSuffix(WikiURL, "/cthrone/wiki") {
		t.Errorf("WikiURL = %q, want the repository's wiki", WikiURL)
	}
}
