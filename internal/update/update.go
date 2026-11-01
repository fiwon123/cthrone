// Package update compares the release this build came from with the newest one
// published, and names the archive a user would download.
//
// It exists so the terminal and the window answer the same question the same
// way: `cthrone update --check` and the About tab's button are two front ends
// over this package, not two implementations that have to be kept in step. The
// rule the rest of the project follows applies here too — one place decides, so
// the answers cannot disagree.
//
// Nothing here installs anything. A check reads one public document and reports
// what it says; replacing a binary is a separate decision (#214), and on a phone
// the user's own install is the only path there is.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// RepositoryURL is where this program comes from: the one address that lets
	// a user find the source, the releases and the issue tracker from inside
	// the app.
	RepositoryURL = "https://github.com/fiwon123/cthrone"

	// ReleasesURL is the release page — where the archives named by ArchiveName
	// are listed. It is what the About tab's button opens.
	ReleasesURL = RepositoryURL + "/releases"

	// LatestReleaseURL is what a check reads: GitHub's newest published
	// release. It is deliberately the public API without a token — the check
	// runs when a user asks, and must never need a credential.
	LatestReleaseURL = "https://api.github.com/repos/fiwon123/cthrone/releases/latest"

	// CheckTimeout bounds the whole check. A window must not wait on a network:
	// five seconds is long enough for a slow link and short enough that a dead
	// one is reported rather than spun on.
	CheckTimeout = 5 * time.Second

	// maxReleaseBody bounds the read. A release payload is a few kilobytes, and
	// a response that is not is not one this app should hold in memory.
	maxReleaseBody = 1 << 20
)

// ErrNoReleaseVersion is reported when the release list answered 200 with a
// document that names no tag.
var ErrNoReleaseVersion = errors.New("the release list named no version")

// LatestTag asks url for the newest release's tag.
//
// The client and the URL are parameters so a caller can point this at an
// httptest server: a test that reaches github.com is a test that fails when the
// network does, and proves nothing about this code when it passes.
func LatestTag(ctx context.Context, client *http.Client, url string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	// The GitHub API answers JSON only when asked; without this header a proxy
	// in front of it can answer an HTML page that parses as an empty release.
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the release list answered %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBody))
	if err != nil {
		return "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("the release list did not read as JSON: %w", err)
	}

	tag := strings.TrimSpace(release.TagName)
	if tag == "" {
		return "", ErrNoReleaseVersion
	}

	return tag, nil
}

// Text is the answer a user is shown: whether this build is behind, and which
// archive they would want.
//
// "Could not check" is never written here: a failure is an error, and the caller
// reports it. This function only speaks for two versions it can read — and a
// build that does not know its own version says so, because comparing vx.x.x
// against a release would call every release newer.
//
// archive is the file ArchiveName named for this build, or empty when the
// release does not publish one for this platform; the sentence then points at
// the release page instead of inventing a name.
func Text(current, latest, archive string) string {
	currentParts, ok := Parse(current)
	if !ok {
		return "This build does not know its version; the latest release is " + latest + "."
	}

	latestParts, ok := Parse(latest)
	if !ok {
		return "The latest release names no version this app can read (" + latest + ")."
	}

	if Compare(currentParts, latestParts) >= 0 {
		return "Up to date (" + current + ")."
	}

	if archive == "" {
		return latest + " is available — this build is " + current + ". The release page has the archive for this platform."
	}

	return latest + " is available — this build is " + current + ". Download " + archive + " from the release page."
}

// Parse reads the leading dotted numbers of a version string: "v0.8.1",
// "0.8.1", and the "v0.8.0-5-gabc123" a `git describe` build reports all read
// as 0.8.1 and 0.8.0. Anything with no leading number is not a version — which
// is what keeps the vx.x.x placeholder from comparing as "older than
// everything".
func Parse(version string) ([]int, bool) {
	return parseVersion(version)
}

// Compare orders two parsed versions the way a user reads them: the missing
// trailing parts of a shorter version are zero, so 0.8 is 0.8.0.
func Compare(a, b []int) int { return compareVersions(a, b) }

// ArchiveName is the file a user downloads for this build, or "" and false when
// the release publishes nothing for it.
//
// The names are the ones `make release` and `make release-gui` produce, and a
// test checks them against the Makefile: a name invented here would send a user
// to a 404, which is worse than saying "look at the release page". gui selects
// the window's archive; the command line and the window are different files on
// every desktop platform.
//
// The archive carries the tag, not the bare number: the files are uploaded as
// `cthrone_v0.9.0_…`, so a name built from "0.9.0" would not exist either.
func ArchiveName(tag string, gui bool, goos, goarch string) (string, bool) {
	// The release matrix is explicit rather than derived: a platform the
	// project does not build for gets no guess.
	names := map[struct {
		gui    bool
		goos   string
		goarch string
	}]string{
		{false, "linux", "amd64"}:   "cthrone_" + tag + "_linux.tar.gz",
		{false, "windows", "amd64"}: "cthrone_" + tag + "_windows.zip",
		{false, "android", "arm64"}: "cthrone_" + tag + "_android.tar.gz",
		{false, "darwin", "arm64"}:  "cthrone_" + tag + "_darwin_arm64.tar.gz",
		{false, "darwin", "amd64"}:  "cthrone_" + tag + "_darwin_amd64.tar.gz",
		{true, "linux", "amd64"}:    "cthrone_" + tag + "_linux_gui_amd64.tar.gz",
		{true, "windows", "amd64"}:  "cthrone_" + tag + "_windows_gui_amd64.zip",
		{true, "darwin", "arm64"}:   "cthrone_" + tag + "_darwin_gui_arm64.tar.gz",
		{true, "android", "arm64"}:  "cthrone_" + tag + "_android_gui_arm64.apk",
	}

	name, ok := names[struct {
		gui    bool
		goos   string
		goarch string
	}{gui, strings.ToLower(strings.TrimSpace(goos)), strings.ToLower(strings.TrimSpace(goarch))}]

	return name, ok
}
