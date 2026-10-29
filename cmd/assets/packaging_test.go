package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// These tests guard the packaging rules that are easy to break and invisible
// until someone extracts a release archive: the launcher icon the installer
// carries, the copy the GUI embeds, and the binary the menu entry runs.

// TestInstallerCarriesTheCurrentIcon checks the base64 PNG inside
// scripts/add_to_path.sh.
//
// The release archives ship no icon file — the window icon is embedded in the
// binary, and the only copy on disk is the one the installer writes from this
// blob. That makes the blob hand-maintained whenever the logo changes, so this
// test decodes it and compares it with the PNG the rest of the build uses. It
// also checks the checksum the installer trusts, because a script that decodes
// the right bytes but compares them with a stale hash fails at install time.
func TestInstallerCarriesTheCurrentIcon(t *testing.T) {
	script := readRepoFile(t, "../../scripts/add_to_path.sh")
	icon := readRepoFile(t, "../../assets/logo-512.png")

	blob := matchOnce(t, `(?m)^CTHRONE_ICON_PNG_B64='([A-Za-z0-9+/=]+)'$`, script)
	if blob == "" {
		t.Fatal("scripts/add_to_path.sh carries no CTHRONE_ICON_PNG_B64 line")
	}

	decoded, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatalf("the embedded icon is not valid base64: %v", err)
	}

	if !bytes.Equal(decoded, icon) {
		t.Errorf("the embedded icon is %d bytes and assets/logo-512.png is %d — "+
			"run the snippet in scripts/add_to_path.sh's comment to refresh it",
			len(decoded), len(icon))
	}

	want := fmt.Sprintf("%x", sha256.Sum256(icon))

	if got := matchOnce(t, `(?m)^CTHRONE_ICON_PNG_SHA256='([0-9a-f]{64})'$`, script); got != want {
		t.Errorf("CTHRONE_ICON_PNG_SHA256 = %q, want %q", got, want)
	}
}

// TestGUIShipsTheSameLogo checks the copy the GUI embeds.
//
// //go:embed cannot reach outside its own package, so ui/ carries a second copy
// of the logo. `make assets` refreshes it; this test notices when it has not
// been refreshed.
func TestGUIShipsTheSameLogo(t *testing.T) {
	source := readRepoFile(t, "../../assets/logo-512.png")
	embedded := readRepoFile(t, "../../ui/logo.png")

	if !bytes.Equal(source, embedded) {
		t.Error("ui/logo.png differs from assets/logo-512.png — run `make assets`")
	}
}

// TestDesktopEntryRunsTheGUI keeps the menu entry pointing at the binary that
// can open a window.
//
// The archive's `cthrone` is the command-line tool: run without the `gui` build
// tag it prints "GUI not built" and exits 0, and the entry sets Terminal=false,
// so the failure is invisible. `v0.5.6` shipped exactly that.
func TestDesktopEntryRunsTheGUI(t *testing.T) {
	entry := readRepoFile(t, "../../scripts/cthrone.desktop")

	if !strings.Contains(string(entry), "\nExec=cthrone_gui\n") {
		t.Error("scripts/cthrone.desktop must run Exec=cthrone_gui")
	}
}

// readRepoFile reads a file relative to the package directory, which is the
// working directory of a test.
func readRepoFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// matchOnce returns the first capture group of pattern in data, or "" when the
// pattern does not match.
func matchOnce(t *testing.T, pattern string, data []byte) string {
	t.Helper()

	match := regexp.MustCompile(pattern).FindSubmatch(data)
	if match == nil {
		return ""
	}

	return string(match[1])
}
