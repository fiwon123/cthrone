package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIdentityCreatesDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// also cover XDG? use UserHomeDir which uses HOME
	id, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if id.Name == "" {
		t.Fatal("name should be set")
	}
	path := filepath.Join(tmp, ".config", "cthrone", "device.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(&Identity{Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	id2, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if id2.Name != "renamed" {
		t.Fatalf("want renamed got %s", id2.Name)
	}
}
