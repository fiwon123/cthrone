//go:build gui

package ui

import (
	"errors"
	"testing"

	"github.com/fiwon123/cthrone/internal/discovery"
)

// withIdentity makes the identity read answer without the real device, and
// counts the calls.
func withIdentity(t *testing.T, name string, err error) *int {
	t.Helper()

	calls := 0

	previous := loadIdentity
	loadIdentity = func() (*discovery.Identity, error) {
		calls++

		if err != nil {
			return nil, err
		}

		return &discovery.Identity{Name: name}, nil
	}
	t.Cleanup(func() { loadIdentity = previous })

	return &calls
}

// Building the window must not read the disk: the identity used to be loaded
// inside build, so the window appeared only after a file read had finished —
// the delay #240 is about.
func TestBuildDoesNotReadTheIdentity(t *testing.T) {
	calls := withIdentity(t, "laptop", nil)

	g := newScreenGUI(t, false)

	if *calls != 0 {
		t.Errorf("build() read the identity %d time(s), want none: it belongs after the window is shown", *calls)
	}

	if got := g.nameEntry.Text; got != "" {
		t.Errorf("the name field reads %q before the identity was filled", got)
	}
}

// The name arrives after the window, and fills the field.
func TestFillIdentityNamesTheDevice(t *testing.T) {
	calls := withIdentity(t, "laptop", nil)

	g := newScreenGUI(t, false)

	if err := g.fillIdentity(); err != nil {
		t.Fatalf("fillIdentity() error = %v", err)
	}

	if *calls != 1 {
		t.Errorf("the identity was read %d time(s), want 1", *calls)
	}

	if got := g.nameEntry.Text; got != "laptop" {
		t.Errorf("the name field reads %q, want the identity's name", got)
	}
}

// A name the user typed beats the one on disk: the identity is a default, not an
// instruction, and it lands after the field already exists.
func TestFillIdentityKeepsWhatTheUserTyped(t *testing.T) {
	withIdentity(t, "laptop", nil)

	g := newScreenGUI(t, false)
	g.nameEntry.SetText("my-name")

	if err := g.fillIdentity(); err != nil {
		t.Fatalf("fillIdentity() error = %v", err)
	}

	if got := g.nameEntry.Text; got != "my-name" {
		t.Errorf("the name field reads %q, want what the user typed", got)
	}
}

// No identity on disk is not an error the user has to see: the field keeps its
// placeholder and the window works.
func TestFillIdentityWithoutOne(t *testing.T) {
	withIdentity(t, "", errors.New("no identity yet"))

	g := newScreenGUI(t, false)

	if err := g.fillIdentity(); err == nil {
		t.Error("fillIdentity() hid the read error, want it returned to the caller's log")
	}

	if got := g.nameEntry.Text; got != "" {
		t.Errorf("the name field reads %q after a failed read", got)
	}
}
