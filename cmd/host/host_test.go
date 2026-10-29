package hostcmd

import (
	"testing"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/pairing"
)

// The encrypted transport is the default, and the default is what a command
// line with no switch at all has to reach: a flag whose default reads right but
// is never passed on would leave the host serving plaintext. The seam replaces
// the listener, so this checks the wiring without binding a port.
func TestHostServesEncryptedWithoutAFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var (
		got    auth.TLS
		called bool
	)

	stubHost(t, func(_ *app.Data, _ pairing.Options, tlsConf auth.TLS) error {
		called, got = true, tlsConf

		return nil
	})

	inheritRootFlags(t)

	Cmd.SetArgs([]string{})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("the websocket host was not started")
	}

	if !got.Enabled {
		t.Errorf("a host with no flags serves plaintext: TLS = %+v", got)
	}
}

// --no-tls is the opt-out, and it has to reach the transport: the switch
// existing on the command line proves nothing about what the host does.
func TestHostOptsOutOfTLS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var (
		got    auth.TLS
		called bool
	)

	stubHost(t, func(_ *app.Data, _ pairing.Options, tlsConf auth.TLS) error {
		called, got = true, tlsConf

		return nil
	})

	inheritRootFlags(t)

	Cmd.SetArgs([]string{"--no-tls"})

	if err := Cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("the websocket host was not started")
	}

	if got.Enabled {
		t.Errorf("--no-tls still enabled TLS: %+v", got)
	}
}

// inheritRootFlags registers the switches the root command makes persistent.
// A command package has no parent in its own test binary, so without them the
// arguments a user would type are rejected before the command runs — and a test
// that cannot pass --no-tls cannot tell whether the flag is wired to anything.
// The scan command's test does the same with --port.
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

// stubHost replaces the websocket host for the duration of a test, so the test
// sees the TLS setting the command resolved without opening a listener.
func stubHost(t *testing.T, replacement func(*app.Data, pairing.Options, auth.TLS) error) {
	t.Helper()

	original := hostWebsocket
	hostWebsocket = replacement

	t.Cleanup(func() {
		hostWebsocket = original
		Cmd.SetArgs([]string{})

		for _, name := range []string{"tls", "no-tls", "cert-fingerprint", "insecure", "port"} {
			if flag := Cmd.Flags().Lookup(name); flag != nil {
				_ = flag.Value.Set(flag.DefValue)
			}
		}
	})
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

// The interface to bind to has to be configurable: a host that cannot be
// narrowed to one interface cannot be run on a machine that is exposed.
func TestBindAddressFlagIsRegistered(t *testing.T) {
	flag := Cmd.Flags().Lookup("host")
	if flag == nil {
		t.Fatal("expected the --host flag to be registered")
	}

	if flag.DefValue != "" {
		t.Errorf("expected every interface by default, got %q", flag.DefValue)
	}
}

// Letting clients in without a secret is the state the tool was in before
// the secret existed. It has to be asked for, and it is a switch.
func TestAllowAnonymousFlagIsASwitch(t *testing.T) {
	flag := Cmd.Flags().Lookup("allow-anonymous")
	if flag == nil {
		t.Fatal("expected the --allow-anonymous flag to be registered")
	}

	if flag.Value.Type() != "bool" {
		t.Errorf("expected --allow-anonymous to be a switch, got %q", flag.Value.Type())
	}

	if flag.DefValue != "false" {
		t.Errorf("expected anonymous connections to be off by default, got %q", flag.DefValue)
	}
}
