//go:build gui

package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

// openRecorder collects the addresses a button asked the browser to open, so a
// test can tell a wired button from a dead one.
type openRecorder struct {
	opened []string
	err    error
}

// withOpener swaps the browser out for the test's recorder.
func withOpener(t *testing.T) *openRecorder {
	t.Helper()

	recorder := &openRecorder{}

	previous := openPageInBrowser
	openPageInBrowser = func(_ fyne.App, link *url.URL) error {
		recorder.opened = append(recorder.opened, link.String())

		return recorder.err
	}
	t.Cleanup(func() { openPageInBrowser = previous })

	return recorder
}

// withCheckURL points the About tab's check at a local answer.
func withCheckURL(t *testing.T, status int, body string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	previous := checkReleaseURL
	checkReleaseURL = server.URL
	t.Cleanup(func() { checkReleaseURL = previous })
}

// The About tab's release button opens the release page, not the repository:
// the point of the button is to reach the archives the note names, and a button
// wired to the wrong address looks identical until somebody taps it.
func TestAboutReleaseButtonOpensTheReleasePage(t *testing.T) {
	g := newScreenGUI(t, false)
	recorder := withOpener(t)

	if g.releaseBtn == nil {
		t.Fatal("the About tab has no release button")
	}

	test.Tap(g.releaseBtn)

	if len(recorder.opened) != 1 {
		t.Fatalf("the button opened %v, want one address", recorder.opened)
	}

	if got, want := recorder.opened[0], ReleasesURL; got != want {
		t.Errorf("the release button opened %q, want %q", got, want)
	}

	// And the repository button still opens the repository, so the two are not
	// the same action twice.
	g.openRepository()

	if got, want := recorder.opened[1], RepositoryURL; got != want {
		t.Errorf("the repository button opened %q, want %q", got, want)
	}
}

// A check that finds a newer release names the window's archive on the tab. This
// is the end the user acts on: "a newer release exists" without saying which
// file to download leaves them guessing.
func TestAboutCheckNamesTheWindowArchive(t *testing.T) {
	withCheckURL(t, http.StatusOK, `{"tag_name":"v0.10.0"}`)

	g := newScreenGUI(t, false)

	// A known build, so the comparison is about the release and not about a
	// placeholder version.
	previousVersion := Version
	Version = "v0.9.0"
	t.Cleanup(func() { Version = previousVersion })

	g.checkUpdates()

	note := waitForAboutNote(t, g, "available")

	archive, ok := updatepkg.ArchiveName("v0.10.0", true, runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("this platform has no window archive in the release matrix")
	}

	if !strings.Contains(note, archive) {
		t.Errorf("the About note = %q, want it to name %q", note, archive)
	}
}

// A failed check is reported as a failure on the tab — never as "up to date",
// and never as a button stuck on "Checking…".
func TestAboutCheckReportsAFailure(t *testing.T) {
	withCheckURL(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)

	g := newScreenGUI(t, false)

	g.checkUpdates()

	note := waitForAboutNote(t, g, "Could not check")

	if strings.Contains(note, "Up to date") {
		t.Errorf("the About note = %q, want no version claim after a failed check", note)
	}

	if g.updateBtn.Disabled() {
		t.Error("the check button stayed disabled after a failure")
	}
}

// waitForAboutNote waits for the background check to write its answer on the
// tab, and returns it. The check runs off the UI goroutine, so the note is empty
// for a moment.
func waitForAboutNote(t *testing.T, g *gui, contains string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if g.aboutNote != nil {
			if note := g.aboutNote.Text(); strings.Contains(note, contains) && note != "Checking…" {
				return note
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("the About note never said %q", contains)

	return ""
}
