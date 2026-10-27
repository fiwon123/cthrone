package ui

import (
	"path/filepath"
	"testing"
)

func TestParsePort(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"normal", "8080", 8080},
		{"padded", "  9000  ", 9000},
		{"lowest", "1", 1},
		{"highest", "65535", 65535},
		{"empty", "", defaultPort},
		{"zero", "0", defaultPort},
		{"too high", "65536", defaultPort},
		{"negative", "-1", defaultPort},
		{"not a number", "chat", defaultPort},
		{"float", "80.5", defaultPort},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePort(tt.text); got != tt.want {
				t.Errorf("parsePort(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestSubjectOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		fallback string
		want     string
	}{
		{"given", "room", "chat", "room"},
		{"padded", "  room  ", "chat", "room"},
		{"empty", "", "chat", "chat"},
		{"blank", "   ", "chat", "chat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subjectOrDefault(tt.text, tt.fallback); got != tt.want {
				t.Errorf("subjectOrDefault(%q, %q) = %q, want %q", tt.text, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestResolveConfigDir(t *testing.T) {
	t.Run("desktop uses the user config directory", func(t *testing.T) {
		config := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", config)

		want := filepath.Join(config, "cthrone")
		if got := resolveConfigDir(false, "/storage/root"); got != want {
			t.Errorf("resolveConfigDir(false, ...) = %q, want %q", got, want)
		}
	})

	t.Run("mobile uses the app storage, not a home directory", func(t *testing.T) {
		config := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", config)

		storage := t.TempDir()
		if got := resolveConfigDir(true, storage); got != storage {
			t.Errorf("resolveConfigDir(true, %q) = %q, want the storage root", storage, got)
		}
	})

	t.Run("desktop falls back to storage when no config directory exists", func(t *testing.T) {
		// os.UserConfigDir errors only when neither XDG_CONFIG_HOME nor HOME is
		// set, which is exactly the state a stripped-down service runs in.
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "")

		storage := t.TempDir()
		if got := resolveConfigDir(false, storage); got != storage {
			t.Errorf("resolveConfigDir(false, %q) = %q, want the storage root", storage, got)
		}
	})

	t.Run("last resort is the working directory", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "")

		if got := resolveConfigDir(true, ""); got != "." {
			t.Errorf("resolveConfigDir(true, \"\") = %q, want %q", got, ".")
		}
	})
}
