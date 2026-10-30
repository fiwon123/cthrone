package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// RepositoryURL is where this program comes from, shown and linked on the
	// About tab. It is the one address that lets a user find the source, the
	// releases and the issue tracker from inside the app.
	RepositoryURL = "https://github.com/fiwon123/cthrone"

	// LatestReleaseURL is what the update check reads: GitHub's newest published
	// release. It is deliberately the public API without a token — the check
	// runs once, on a button, and must never need a credential.
	LatestReleaseURL = "https://api.github.com/repos/fiwon123/cthrone/releases/latest"

	// updateCheckTimeout bounds the whole check. A window must not wait on a
	// network: five seconds is long enough for a slow link and short enough
	// that a dead one is reported rather than spun on.
	updateCheckTimeout = 5 * time.Second

	// maxReleaseBody bounds the read. A release payload is a few kilobytes, and
	// a response that is not is not one this app should hold in memory.
	maxReleaseBody = 1 << 20

	// zeroMetadataVersion is the version Fyne reports when nothing set one. It
	// is not this program's version, so it is treated as absent.
	zeroMetadataVersion = "0.0.1"
)

// errNoReleaseVersion is reported when the release list answered 200 with a
// document that names no tag.
var errNoReleaseVersion = errors.New("the release list named no version")

// latestReleaseTag asks url for the newest release's tag.
//
// The client and the URL are parameters so a test can point this at an
// httptest server: a test that reaches github.com is a test that fails when the
// network does, and proves nothing about this code when it passes.
func latestReleaseTag(ctx context.Context, client *http.Client, url string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

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
		return "", errNoReleaseVersion
	}

	return tag, nil
}

// updateText turns the two versions into the sentence the About tab shows.
//
// "Could not check" is never written here: a failure is an error, and the
// caller reports it. This function only speaks for two versions it can read —
// and a build that does not know its own version says so, because comparing
// vx.x.x against a release would call every release newer.
func updateText(current, latest string) string {
	currentParts, ok := parseVersion(current)
	if !ok {
		return "This build does not know its version; the latest release is " + latest + "."
	}

	latestParts, ok := parseVersion(latest)
	if !ok {
		return "The latest release names no version this app can read (" + latest + ")."
	}

	if compareVersions(currentParts, latestParts) < 0 {
		return latest + " is available — this build is " + current + "."
	}

	return "Up to date (" + current + ")."
}

// aboutVersion is the version string the About tab reports.
//
// The injected value wins. Without one — a local build, or the APK, where the
// packager sets the app version instead of this linker line — Fyne's own
// metadata is the next best answer, because `fyne package -app-version` writes
// it. *That* being absent too is vx.x.x, the same convention as the CLI: an
// unknown version says so rather than guessing.
func aboutVersion(injected, metadata string) string {
	if injected != "" && injected != placeholderVersion {
		return injected
	}

	version := strings.TrimSpace(metadata)
	version = strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")

	if version != "" && version != zeroMetadataVersion {
		return "v" + version
	}

	return placeholderVersion
}

// parseVersion reads the leading dotted numbers of a version string: "v0.8.1",
// "0.8.1", and the "v0.8.0-5-gabc123" a `git describe` build reports all read
// as 0.8.1 and 0.8.0. Anything with no leading number is not a version.
func parseVersion(version string) ([]int, bool) {
	version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")

	numeric := version
	if end := strings.IndexFunc(version, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}); end >= 0 {
		numeric = version[:end]
	}

	numeric = strings.Trim(numeric, ".")
	if numeric == "" {
		return nil, false
	}

	fields := strings.Split(numeric, ".")
	parts := make([]int, len(fields))
	for i, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return nil, false
		}

		parts[i] = value
	}

	return parts, true
}

// compareVersions compares two parsed versions the way a user reads them: the
// missing trailing parts of a shorter version are zero, so 0.8 is 0.8.0.
func compareVersions(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}

	for i := 0; i < length; i++ {
		left, right := 0, 0
		if i < len(a) {
			left = a[i]
		}
		if i < len(b) {
			right = b[i]
		}

		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		}
	}

	return 0
}
