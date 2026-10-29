package app

import "testing"

func TestNew(t *testing.T) {
	d := New(8080)
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Port != 8080 {
		t.Errorf("Port = %d, want 8080", d.Port)
	}
	if d.Host != "" {
		t.Errorf("Host = %q, want empty", d.Host)
	}
	if d.Name != "" {
		t.Errorf("Name = %q, want empty", d.Name)
	}
}
