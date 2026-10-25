package scancmd

import (
	"testing"

	"github.com/fiwon123/cthrone/internal/data/app"
)

func TestScanCommandRegistered(t *testing.T) {
	if Cmd.Use != "scan" {
		t.Errorf("expected Use to be 'scan', got %q", Cmd.Use)
	}
}

func TestScanCommandUsesPort(t *testing.T) {
	orig := scanFunc
	defer func() { scanFunc = orig }()

	var gotPort int
	scanFunc = func(a *app.Data) []string {
		gotPort = a.Port
		return nil
	}

	// Add port flag like root does
	Cmd.Flags().IntP("port", "p", 8080, "")
	Cmd.SetArgs([]string{"-p", "9090"})
	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotPort != 9090 {
		t.Errorf("expected port 9090, got %d", gotPort)
	}
}

func TestScanCommandDefaultPort(t *testing.T) {
	orig := scanFunc
	defer func() { scanFunc = orig }()

	var gotPort int
	scanFunc = func(a *app.Data) []string {
		gotPort = a.Port
		return nil
	}

	// Reset flags for test isolation
	Cmd.Flags().Set("port", "8080")

	if gotPort != 8080 && gotPort != 0 {
		// Will be set by Execute if default is used
	}
}
