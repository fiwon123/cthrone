package ui

import (
	"runtime"
	"strings"

	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

const (
	// RepositoryURL is where this program comes from, shown and linked on the
	// About tab. It is the one address that lets a user find the source, the
	// releases and the issue tracker from inside the app.
	//
	// The value lives in internal/update because the terminal's check needs the
	// same address; two copies of a URL that has to agree are a typo waiting to
	// happen.
	RepositoryURL = updatepkg.RepositoryURL

	// ReleasesURL is the release page: what the About tab's button opens, and
	// where the archives the note names are listed.
	ReleasesURL = updatepkg.ReleasesURL

	// zeroMetadataVersion is the version Fyne reports when nothing set one. It
	// is not this program's version, so it is treated as absent.
	zeroMetadataVersion = "0.0.1"
)

// aboutUpdateText is the update check's answer as the About tab shows it: the
// shared sentence, with the archive a window user would download named in it.
//
// The archive is the GUI's, not the command line's — the two are different
// files on every desktop platform, and a user reading a window is the one who
// wants the window build.
func aboutUpdateText(current, latest string) string {
	archive, _ := updatepkg.ArchiveName(latest, true, runtime.GOOS, runtime.GOARCH)

	return updatepkg.Text(current, latest, archive)
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
