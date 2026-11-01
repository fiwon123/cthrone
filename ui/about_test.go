package ui

import (
	"strings"
	"testing"
)

// The About tab names the archive a window user would download, and it is the
// window's — not the command line's. The two are different files on every
// desktop platform, so a tab that named the CLI tarball would send a GUI user to
// the wrong one.
func TestAboutUpdateTextNamesTheWindowArchive(t *testing.T) {
	got := aboutUpdateText("v0.9.0", "v0.10.0")

	if !strings.Contains(got, "v0.10.0 is available") {
		t.Fatalf("aboutUpdateText() = %q, want it to report the newer release", got)
	}

	if !strings.Contains(got, "_gui_") {
		t.Errorf("aboutUpdateText() = %q, want the window archive named", got)
	}

	if strings.Contains(got, "cthrone_v0.10.0_linux.tar.gz") {
		t.Errorf("aboutUpdateText() = %q, want the window archive, not the command line's", got)
	}

	// The version the tab is running stays in the sentence: it is what the user
	// quotes in a bug report.
	if !strings.Contains(got, "this build is v0.9.0") {
		t.Errorf("aboutUpdateText() = %q, want the running version named", got)
	}
}

// An unknown build is never told it is current, and the tab does not invent an
// archive for a release it cannot read.
func TestAboutUpdateTextRefusesToGuess(t *testing.T) {
	got := aboutUpdateText(placeholderVersion, "v0.10.0")

	if strings.Contains(got, "Up to date") {
		t.Errorf("aboutUpdateText() = %q, want it not to claim the build is current", got)
	}

	if strings.Contains(got, ".tar.gz") || strings.Contains(got, ".apk") {
		t.Errorf("aboutUpdateText() = %q, want no archive named for a build it cannot compare", got)
	}
}

// The injected version wins; without one the packager's metadata is the next
// answer, because that is how the APK is versioned. Fyne's "0.0.1" placeholder
// is not a version this program shipped.
func TestAboutVersion(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		metadata string
		want     string
	}{
		{"injected", "v0.8.1", "0.9.0", "v0.8.1"},
		{"the packager's version", placeholderVersion, "0.8.1", "v0.8.1"},
		{"the packager's version already prefixed", placeholderVersion, "v0.8.1", "v0.8.1"},
		{"no injected version, no metadata", placeholderVersion, "", placeholderVersion},
		{"Fyne's placeholder is not a version", placeholderVersion, zeroMetadataVersion, placeholderVersion},
		{"an empty injected value", "", "", placeholderVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aboutVersion(tt.injected, tt.metadata); got != tt.want {
				t.Errorf("aboutVersion(%q, %q) = %q, want %q", tt.injected, tt.metadata, got, tt.want)
			}
		})
	}
}
