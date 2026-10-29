package connectcmd

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
