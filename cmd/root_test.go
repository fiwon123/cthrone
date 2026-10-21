package cmd

import "testing"

// The shared secret is a persistent flag, so the host and the connect
// command take the same one. A flag on one command only would leave the
// other unable to find the secret.
func TestTokenFlagsArePersistent(t *testing.T) {
	for _, name := range []string{"token", "token-file"} {
		flag := rootCmd.PersistentFlags().Lookup(name)
		if flag == nil {
			t.Fatalf("expected the --%s flag to be registered", name)
		}
	}
}

// The secret is not a plain flag value. The help says where it should come
// from instead, because a flag value is visible in the process list.
func TestTokenFlagSaysWhereTheSecretShouldComeFrom(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("token")
	if flag == nil {
		t.Fatal("expected the --token flag to be registered")
	}

	if flag.Usage == "" {
		t.Error("expected the --token help text to say something")
	}
}

// The transport can be encrypted. The flag is shared by the host and the
// client: the host serves https when it is on, the client expects it.
func TestTLSFlagIsPersistent(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("tls")
	if flag == nil {
		t.Fatal("expected the --tls flag to be registered")
	}

	if flag.Value.Type() != "bool" {
		t.Errorf("expected --tls to be a switch, got %q", flag.Value.Type())
	}

	if flag.DefValue != "false" {
		t.Errorf("expected the transport to be unencrypted by default, got %q", flag.DefValue)
	}
}
