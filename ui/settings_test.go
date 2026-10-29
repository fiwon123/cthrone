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

func TestSetupVisibility(t *testing.T) {
	tests := []struct {
		name        string
		useNATS     bool
		tls         bool
		moreOptions bool
		want        setupView
	}{
		{
			name: "websocket, plain",
			want: setupView{Security: true},
		},
		{
			name: "websocket, TLS adds the insecure row",
			tls:  true,
			want: setupView{Security: true, Insecure: true},
		},
		{
			name:        "websocket, more options opens the advanced rows",
			moreOptions: true,
			want:        setupView{Security: true, SharedSecret: true, BindAddress: true},
		},
		{
			name:        "websocket, more options with TLS opens every row",
			tls:         true,
			moreOptions: true,
			want: setupView{
				Security:     true,
				SharedSecret: true,
				Fingerprint:  true,
				BindAddress:  true,
				Insecure:     true,
			},
		},
		{
			name:    "NATS hides the websocket security row",
			useNATS: true,
			want:    setupView{NATSBroker: true, NATSSubject: true},
		},
		{
			name:    "NATS with TLS keeps the insecure row",
			useNATS: true,
			tls:     true,
			want:    setupView{Insecure: true, NATSBroker: true, NATSSubject: true},
		},
		{
			name:        "NATS ignores the advanced websocket rows",
			useNATS:     true,
			moreOptions: true,
			want:        setupView{NATSBroker: true, NATSSubject: true},
		},
		{
			name:        "NATS with everything on still shows only broker rows",
			useNATS:     true,
			tls:         true,
			moreOptions: true,
			want:        setupView{Insecure: true, NATSBroker: true, NATSSubject: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setupVisibility(tt.useNATS, tt.tls, tt.moreOptions)
			if got != tt.want {
				t.Errorf("setupVisibility(%v, %v, %v) = %+v, want %+v",
					tt.useNATS, tt.tls, tt.moreOptions, got, tt.want)
			}
		})
	}
}

func TestSetupHint(t *testing.T) {
	websocket := setupHint(false)
	nats := setupHint(true)

	if websocket == "" || nats == "" {
		t.Fatalf("setupHint returned an empty hint: websocket %q, NATS %q", websocket, nats)
	}

	if websocket == nats {
		t.Errorf("setupHint is the same for both transports: %q", websocket)
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
