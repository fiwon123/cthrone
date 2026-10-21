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

// The certificate is pinned by its fingerprint. The host's certificate is
// self-signed, so there is no authority to check it against.
func TestCertFingerprintFlagIsRegistered(t *testing.T) {
	flag := Cmd.Flags().Lookup("cert-fingerprint")
	if flag == nil {
		t.Fatal("expected the --cert-fingerprint flag to be registered")
	}

	if flag.DefValue != "" {
		t.Errorf("expected no fingerprint by default, got %q", flag.DefValue)
	}
}

// Accepting any certificate is a switch, and it is off by default.
func TestInsecureFlagIsASwitch(t *testing.T) {
	flag := Cmd.Flags().Lookup("insecure")
	if flag == nil {
		t.Fatal("expected the --insecure flag to be registered")
	}

	if flag.Value.Type() != "bool" {
		t.Errorf("expected --insecure to be a switch, got %q", flag.Value.Type())
	}

	if flag.DefValue != "false" {
		t.Errorf("expected the certificate to be checked by default, got %q", flag.DefValue)
	}
}
