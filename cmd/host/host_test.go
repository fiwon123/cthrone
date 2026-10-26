package hostcmd

import (
	"testing"

	natscore "github.com/fiwon123/cthrone/internal/core/nats"
)

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
