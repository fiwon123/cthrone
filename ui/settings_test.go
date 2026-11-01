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
		mode        transportMode
		tls         bool
		moreOptions bool
		want        setupView
	}{
		{
			name: "websocket, plain",
			want: setupView{Port: true, Options: true},
		},
		{
			name: "websocket, TLS adds the insecure row",
			tls:  true,
			want: setupView{Port: true, Options: true, Insecure: true},
		},
		{
			name:        "websocket, more options opens the advanced rows",
			moreOptions: true,
			want:        setupView{Port: true, Options: true, SharedSecret: true, BindAddress: true},
		},
		{
			name:        "websocket, more options with TLS opens every row",
			tls:         true,
			moreOptions: true,
			want: setupView{
				Port:         true,
				Options:      true,
				SharedSecret: true,
				Fingerprint:  true,
				BindAddress:  true,
				Insecure:     true,
			},
		},
		{
			name: "NATS hides the websocket options row",
			mode: transportNATS,
			want: setupView{Port: true, NATSBroker: true, NATSSubject: true},
		},
		{
			name: "NATS with TLS keeps the insecure row",
			mode: transportNATS,
			tls:  true,
			want: setupView{Port: true, Insecure: true, NATSBroker: true, NATSSubject: true},
		},
		{
			name:        "NATS ignores the advanced websocket rows",
			mode:        transportNATS,
			moreOptions: true,
			want:        setupView{Port: true, NATSBroker: true, NATSSubject: true},
		},
		{
			name:        "NATS with everything on still shows only broker rows",
			mode:        transportNATS,
			tls:         true,
			moreOptions: true,
			want:        setupView{Port: true, Insecure: true, NATSBroker: true, NATSSubject: true},
		},
		{
			// The radio has no port, no bind address and no certificate to pin:
			// the rows that configure a socket or a TLS connection would each
			// be a setting the transport cannot use.
			name: "Bluetooth keeps the transport row and nothing else",
			mode: transportBluetooth,
			want: setupView{Options: true},
		},
		{
			// A session on the radio is sealed and authenticated like any other,
			// so the credential row is the one advanced row it keeps.
			name:        "Bluetooth keeps the credential row",
			mode:        transportBluetooth,
			moreOptions: true,
			want:        setupView{Options: true, SharedSecret: true},
		},
		{
			// TLS belongs to the websocket connection. A Bluetooth link is not
			// a socket, so the checkbox that spares one its certificate check
			// means nothing there.
			name: "Bluetooth ignores TLS",
			mode: transportBluetooth,
			tls:  true,
			want: setupView{Options: true},
		},
		{
			name:        "Bluetooth with everything on shows no network row",
			mode:        transportBluetooth,
			tls:         true,
			moreOptions: true,
			want:        setupView{Options: true, SharedSecret: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setupVisibility(tt.mode, tt.tls, tt.moreOptions)
			if got != tt.want {
				t.Errorf("setupVisibility(%v, %v, %v) = %+v, want %+v",
					tt.mode, tt.tls, tt.moreOptions, got, tt.want)
			}
		})
	}
}

func TestSetupHint(t *testing.T) {
	hints := []string{
		setupHint(transportWebsocket),
		setupHint(transportNATS),
		setupHint(transportBluetooth),
	}

	seen := make(map[string]bool, len(hints))

	for i, hint := range hints {
		if hint == "" {
			t.Fatalf("setupHint for transport %d is empty", i)
		}

		// One sentence cannot serve three transports: a hint that is the same
		// for two of them is not describing either.
		if seen[hint] {
			t.Errorf("setupHint returns %q for more than one transport", hint)
		}

		seen[hint] = true
	}
}

// The transport select is the one control, so a mode has to survive the trip
// through its option text and back — including the empty selection a select
// reports before it is first shown.
func TestTransportOfRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		selected string
		want     transportMode
	}{
		{"websocket", labelWebsocket, transportWebsocket},
		{"NATS", labelNATS, transportNATS},
		{"Bluetooth", labelBluetooth, transportBluetooth},
		{"nothing selected yet", "", transportWebsocket},
		{"an option this build does not offer", "Carrier pigeon", transportWebsocket},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transportOf(tt.selected); got != tt.want {
				t.Fatalf("transportOf(%q) = %v, want %v", tt.selected, got, tt.want)
			}
		})
	}

	for _, mode := range []transportMode{transportWebsocket, transportNATS, transportBluetooth} {
		if got := transportOf(transportName(mode)); got != mode {
			t.Errorf("transportName(%v) does not survive the round trip: %q -> %v",
				mode, transportName(mode), got)
		}
	}
}

// The select offers the transports this build can run, and only those: a build
// without the radio does not show an option that could only fail.
func TestTransportNames(t *testing.T) {
	tests := []struct {
		name      string
		bluetooth bool
		want      []string
	}{
		{"a build with the radio", true, []string{labelWebsocket, labelNATS, labelBluetooth}},
		{"a build without it", false, []string{labelWebsocket, labelNATS}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transportNames(tt.bluetooth)

			if len(got) != len(tt.want) {
				t.Fatalf("transportNames(%v) = %v, want %v", tt.bluetooth, got, tt.want)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("transportNames(%v) = %v, want %v", tt.bluetooth, got, tt.want)
				}
			}

			// The radio is never the default and never the first option: a
			// device that hosts is found by a scan, which is what the top of
			// the list has to stay able to do.
			if len(got) > 0 && got[0] != labelWebsocket {
				t.Errorf("transportNames(%v) starts with %q, want %q", tt.bluetooth, got[0], labelWebsocket)
			}
		})
	}
}

// The label no longer qualifies the platform: the option is only offered by a
// build that has the transport, so "(Linux)" would be noise on the one platform
// that shows it and wrong on a platform that gained it later.
func TestBluetoothLabelNamesNoPlatform(t *testing.T) {
	if strings.Contains(labelBluetooth, "Linux") {
		t.Errorf("the transport is labelled %q, want no platform qualifier", labelBluetooth)
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
