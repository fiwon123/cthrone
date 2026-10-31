package ui

import (
	"os"
	"path/filepath"
	"strings"
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
			want: setupView{Options: true},
		},
		{
			name: "websocket, TLS adds the insecure row",
			tls:  true,
			want: setupView{Options: true, Insecure: true},
		},
		{
			name:        "websocket, more options opens the advanced rows",
			moreOptions: true,
			want:        setupView{Options: true, SharedSecret: true, BindAddress: true},
		},
		{
			name:        "websocket, more options with TLS opens every row",
			tls:         true,
			moreOptions: true,
			want: setupView{
				Options:      true,
				SharedSecret: true,
				Fingerprint:  true,
				BindAddress:  true,
				Insecure:     true,
			},
		},
		{
			name:    "NATS hides the websocket options row",
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

// The row that dials a host is not this device's address, and the label must
// not read as though it were: a user who hosts is shown an empty box there.
func TestConnectToLabel(t *testing.T) {
	if labelConnectTo != "Connect to" {
		t.Errorf("labelConnectTo = %q, want %q", labelConnectTo, "Connect to")
	}
}

func TestDeviceAddresses(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
		port  int
		want  string
	}{
		{"one address", []string{"192.168.1.20"}, 8080, "192.168.1.20:8080"},
		{"several addresses", []string{"192.168.1.20", "10.0.5.7"}, 9000, "192.168.1.20:9000, 10.0.5.7:9000"},
		{"no address", nil, 8080, ""},
		{"empty list", []string{}, 8080, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceAddresses(tt.addrs, tt.port); got != tt.want {
				t.Errorf("deviceAddresses(%v, %d) = %q, want %q", tt.addrs, tt.port, got, tt.want)
			}
		})
	}
}

// The line a hosting device reads is the address the other device's scan finds,
// because the Devices list no longer lists the host itself.
func TestThisDeviceText(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
		port  int
		want  string
	}{
		{"one address", []string{"192.168.1.20"}, 8080, "This device — 192.168.1.20:8080"},
		{"several addresses", []string{"192.168.1.20", "10.0.5.7"}, 8080, "This device — 192.168.1.20:8080, 10.0.5.7:8080"},
		{"no address", nil, 8080, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := thisDeviceText(tt.addrs, tt.port); got != tt.want {
				t.Errorf("thisDeviceText(%v, %d) = %q, want %q", tt.addrs, tt.port, got, tt.want)
			}
		})
	}
}

// The Setup tab names the credential in force and where it came from, and says
// plainly that a token file an older version read no longer decides anything:
// a user who is told that is the one who can stop looking for the cause of a
// connection that will not complete in the wrong place.
func TestCredentialText(t *testing.T) {
	tests := []struct {
		name    string
		typed   string
		envSet  bool
		path    string
		legacy  bool
		want    string
		notWant []string
	}{
		{
			name:    "the device key is the default",
			want:    "this device's key",
			notWant: []string{"shared secret"},
		},
		{
			name:  "what is typed wins",
			typed: "typed-secret",
			want:  "typed above",
		},
		{
			name:   "what is typed wins over the old file",
			typed:  "typed-secret",
			path:   "/home/u/.config/cthrone/token",
			legacy: true,
			want:   "typed above",
		},
		{
			name:   "the environment beats the old file",
			envSet: true,
			path:   "/home/u/.config/cthrone/token",
			legacy: true,
			want:   "CTHRONE_TOKEN",
		},
		{
			name:   "the old file is named and reported inert",
			path:   "/home/u/.config/cthrone/token",
			legacy: true,
			want:   "/home/u/.config/cthrone/token",
		},
		{
			name: "an old file that is not there is not mentioned",
			path: "/home/u/.config/cthrone/token",
			want: "this device's key",
		},
		{
			name:  "blank typing is not a secret",
			typed: "   ",
			want:  "this device's key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := credentialText(tt.typed, tt.envSet, tt.path, tt.legacy)

			if !strings.Contains(got, tt.want) {
				t.Errorf("credentialText() = %q, want it to name %q", got, tt.want)
			}

			for _, unwanted := range tt.notWant {
				if strings.Contains(got, unwanted) {
					t.Errorf("credentialText() = %q, want it not to name %q", got, unwanted)
				}
			}

			// The value of a secret never reaches this line, whatever the
			// source: a screen is copyable, a screenshot is permanent, and the
			// one thing that must not be either is the secret itself.
			if typed := strings.TrimSpace(tt.typed); typed != "" && strings.Contains(got, typed) {
				t.Errorf("credentialText() carries what was typed: %q", got)
			}
		})
	}
}

// The "Delete the old token file" button is offered whenever that file is
// still there, so its presence has to be detectable without reading a secret:
// nothing decides the credential by a file any more.
func TestSecretFileExists(t *testing.T) {
	dir := t.TempDir()

	withSecret := filepath.Join(dir, "token")
	if err := os.WriteFile(withSecret, []byte("  "+strings.Repeat("s", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"a file with a secret", withSecret, true},
		{"an empty file", empty, true},
		{"a missing file", filepath.Join(dir, "absent"), false},
		{"no path at all", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secretFileExists(tt.path); got != tt.want {
				t.Errorf("secretFileExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
