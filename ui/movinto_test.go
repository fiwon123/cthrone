//go:build gui

package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// moveInto writes a received file under the name the peer chose. That name has
// been through the wire's check, but the helper must not depend on a check two
// packages away: a name with a separator in it must not write outside the
// folder it was given.
func TestMoveIntoRefusesAPathAsAName(t *testing.T) {
	staging := t.TempDir()
	inside := t.TempDir()
	outside := filepath.Join(filepath.Dir(inside), "escaped.txt")

	source := filepath.Join(staging, "staged")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../escaped.txt", "..\\escaped.txt", "/tmp/escaped.txt"} {
		_, err := moveInto(source, inside, name)
		if err == nil {
			t.Fatalf("moveInto accepted %q as a name", name)
		}

		if _, statErr := os.Stat(outside); statErr == nil {
			t.Fatalf("moveInto wrote %q outside the folder it was given", outside)
		}
	}

	// A plain name still works.
	if _, err := moveInto(source, inside, "fine.txt"); err != nil {
		t.Fatalf("moveInto refused an ordinary name: %v", err)
	}
}
