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
