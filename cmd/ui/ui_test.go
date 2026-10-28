package uicmd

import "testing"

func TestCmdExists(t *testing.T) {
	if Cmd == nil {
		t.Fatal("Cmd is nil")
	}
	if Cmd.Use != "ui" {
		t.Fatalf("Use = %q", Cmd.Use)
	}
}
