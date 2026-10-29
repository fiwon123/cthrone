package connectcmd

import (
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
)

// The client has to dial wss:// with no flag, because a client that defaults to
// plaintext leaves the connection unencrypted even when the host does not. The
// dialer is replaced here, so this checks what the command resolved without a
// host to reach.
func TestConnectDialsEncryptedWithoutAFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var (
		got    auth.TLS
		called bool
	)

	stubConnect(t, func(_ []string, _ *app.Data, _ pairing.Options, tlsConf auth.TLS) error {
		called, got = true, tlsConf

		return nil
	})

	inheritRootFlags(t)

	Cmd.SetArgs([]string{"host.example:8080"})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("the websocket client was not started")
	}

	if !got.Enabled {
		t.Errorf("a client with no flags dials plaintext: TLS = %+v", got)
	}
}

// --no-tls is the opt-out on the client side too: it has to reach the dialer,
// not only the host.
func TestConnectOptsOutOfTLS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var (
		got    auth.TLS
		called bool
	)

	stubConnect(t, func(_ []string, _ *app.Data, _ pairing.Options, tlsConf auth.TLS) error {
		called, got = true, tlsConf

		return nil
	})

	inheritRootFlags(t)

	Cmd.SetArgs([]string{"--no-tls", "host.example:8080"})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("the websocket client was not started")
	}

	if got.Enabled {
		t.Errorf("--no-tls still dialed TLS: %+v", got)
	}
}

// stubConnect replaces the dialer for the duration of a test and puts the
// command back the way it was found.
func stubConnect(t *testing.T, replacement func([]string, *app.Data, pairing.Options, auth.TLS) error) {
	t.Helper()

	original := connectWebsocket
	connectWebsocket = replacement

	t.Cleanup(func() {
		connectWebsocket = original
		Cmd.SetArgs([]string{})

		for _, name := range []string{"tls", "no-tls", "cert-fingerprint", "insecure", "port"} {
			if flag := Cmd.Flags().Lookup(name); flag != nil {
				_ = flag.Value.Set(flag.DefValue)
			}
		}
	})
}

// inheritRootFlags registers the switches the root command makes persistent.
// A command package has no parent in its own test binary, so without them the
// arguments a user would type are rejected before the command runs. The scan
// command's test does the same with --port.
func inheritRootFlags(t *testing.T) {
	t.Helper()

	if Cmd.Flags().Lookup("no-tls") != nil {
		return
	}

	Cmd.Flags().Bool("tls", false, "")
	Cmd.Flags().Bool("no-tls", false, "")
	Cmd.Flags().String("cert-fingerprint", "", "")
	Cmd.Flags().Bool("insecure", false, "")
	Cmd.Flags().Int("port", 8080, "")
	Cmd.Flags().String("token", "", "")
	Cmd.Flags().String("token-file", "", "")
}

// The broker the NATS transport connects to has to be configurable: the
// flag is what makes a remote broker reachable at all.
func TestNatsURLFlagIsRegisteredWithTheDefaultBroker(t *testing.T) {
	flag := Cmd.Flags().Lookup("nats-url")
	if flag == nil {
		t.Fatal("expected the --nats-url flag to be registered")
	}

	if flag.DefValue != natscore.DefaultBrokerURL {
		t.Errorf("expected the default broker to be %q, got %q", natscore.DefaultBrokerURL, flag.DefValue)
	}
}

// A connecting device carries a name too: the host's status line and the rows
// it prints identify the peer by the name announced in the handshake, and an
// unnamed client can only be called "an unnamed peer".
func TestNameFlagIsRegistered(t *testing.T) {
	flag := Cmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("expected the --name flag to be registered")
	}

	if flag.Value.Type() != "string" {
		t.Errorf("expected --name to take a value, got %q", flag.Value.Type())
	}
}

// The transport switch says what it does. "host a nats server" described a
// URL, and the flag is a switch.
func TestNatsFlagDescribesTheTransport(t *testing.T) {
	flag := Cmd.Flags().Lookup("nats")
	if flag == nil {
		t.Fatal("expected the --nats flag to be registered")
	}

	if flag.Value.Type() != "bool" {
		t.Errorf("expected --nats to be a switch, got %q", flag.Value.Type())
	}

	if flag.Usage == "host a nats server" {
		t.Error("the --nats help text still describes a server instead of the transport")
	}
}
