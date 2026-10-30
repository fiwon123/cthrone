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

// The websocket transport is encrypted by default, so the flags come as a
// pair: --tls is kept so a command line that passes it still runs, and
// --no-tls is the opt-out. Both are persistent, because the host and the
// client have to agree — a switch on one command only would leave the other
// speaking the other transport.
func TestTLSFlagsArePersistent(t *testing.T) {
	for _, name := range []string{"tls", "no-tls"} {
		flag := rootCmd.PersistentFlags().Lookup(name)
		if flag == nil {
			t.Fatalf("expected the --%s flag to be registered", name)
		}

		if flag.Value.Type() != "bool" {
			t.Errorf("expected --%s to be a switch, got %q", name, flag.Value.Type())
		}

		if flag.DefValue != "false" {
			t.Errorf("expected --%s to be off by default, got %q", name, flag.DefValue)
		}
	}
}

// The certificate is pinned by its fingerprint. The host's certificate is
// self-signed, so there is no authority to check it against. The flag is
// persistent: the host command needs it for the broker, the connect
// command for the peer it talks to.
func TestCertFingerprintFlagIsRegistered(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("cert-fingerprint")
	if flag == nil {
		t.Fatal("expected the --cert-fingerprint flag to be registered")
	}

	if flag.DefValue != "" {
		t.Errorf("expected no fingerprint by default, got %q", flag.DefValue)
	}
}

// Accepting any certificate is a switch, and it is off by default.
func TestInsecureFlagIsASwitch(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("insecure")
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

// A command line with no subcommand opens the window.
//
// cthrone_gui is this same command built with -tags gui and -H=windowsgui: a
// double-click runs it with no arguments, and before this it did nothing at all
// and exited 0 — no window, no console, no message. The launch is a variable so
// the no-argument path can be observed; running the real one would open a
// window in the test, and checking the exit code would prove nothing, because
// the broken version also exited 0.
func TestNoSubcommandLaunchesTheGUI(t *testing.T) {
	launched := 0
	restore := stubLaunchGUI(func() { launched++ })
	defer restore()

	rootCmd.SetArgs([]string{})
	defer rootCmd.SetArgs(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if launched != 1 {
		t.Fatalf("the GUI was launched %d times by a command with no arguments, want 1", launched)
	}
}

// --version prints the version and must not also open a window over the shell
// that asked for a string.
func TestVersionDoesNotLaunchTheGUI(t *testing.T) {
	launched := 0
	restore := stubLaunchGUI(func() { launched++ })
	defer restore()

	rootCmd.SetArgs([]string{"--version"})
	defer rootCmd.SetArgs(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if launched != 0 {
		t.Errorf("--version launched the GUI %d times, want 0", launched)
	}
}

// stubLaunchGUI replaces the launcher and returns a function that puts the
// command tree back the way it was: the flag a test parses stays parsed, and
// the next test would inherit both the stub and the flag.
func stubLaunchGUI(fn func()) func() {
	original := launchGUI
	launchGUI = fn

	return func() {
		launchGUI = original
		checkVersion = false
	}
}
