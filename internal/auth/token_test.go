package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// The flag wins over the environment, and the environment over the file:
// the more explicit the source, the more it was meant for this run.
func TestResolvePrefersTheMostExplicitSource(t *testing.T) {
	t.Setenv(EnvToken, "from-env")

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		flagValue  string
		wantValue  string
		wantSource string
	}{
		{
			name:       "the flag wins",
			flagValue:  "from-flag",
			wantValue:  "from-flag",
			wantSource: SourceFlag,
		},
		{
			name:       "the environment comes next",
			flagValue:  "",
			wantValue:  "from-env",
			wantSource: SourceEnvironment,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := Resolve(tc.flagValue, file)
			if err != nil {
				t.Fatal(err)
			}

			if token.Value != tc.wantValue {
				t.Errorf("expected %q, got %q", tc.wantValue, token.Value)
			}

			if token.Source != tc.wantSource {
				t.Errorf("expected the source to be %q, got %q", tc.wantSource, token.Source)
			}
		})
	}
}

// With no flag and no environment, the file is the source.
func TestResolveFallsBackToTheFile(t *testing.T) {
	t.Setenv(EnvToken, "")

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := Resolve("", file)
	if err != nil {
		t.Fatal(err)
	}

	if token.Value != "from-file" {
		t.Errorf("expected the file to be read, got %q", token.Value)
	}

	if token.Source != SourceFile {
		t.Errorf("expected the source to be %q, got %q", SourceFile, token.Source)
	}
}

// A file that does not exist is not an error: it means there is no secret
// yet, which the caller may want to create.
func TestResolveWithoutASecret(t *testing.T) {
	t.Setenv(EnvToken, "")

	token, err := Resolve("", filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("expected a missing file to be no error, got %v", err)
	}

	if token.Value != "" {
		t.Errorf("expected no secret, got %q", token.Value)
	}

	if token.Source != SourceNone {
		t.Errorf("expected the source to be %q, got %q", SourceNone, token.Source)
	}
}

// The secret is written for its owner only. A file every process on the
// machine can read is not a secret.
func TestWriteFileIsNotReadableByOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "token")

	if err := WriteFile(path, "s3cret"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}

// The secret is read back with the newline a shell adds trimmed off.
func TestWriteFileAndReadItBack(t *testing.T) {
	t.Setenv(EnvToken, "")

	path := filepath.Join(t.TempDir(), "token")
	if err := WriteFile(path, "s3cret"); err != nil {
		t.Fatal(err)
	}

	token, err := Resolve("", path)
	if err != nil {
		t.Fatal(err)
	}

	if token.Value != "s3cret" {
		t.Errorf("expected %q, got %q", "s3cret", token.Value)
	}
}

// Two generated secrets are not the same.
func TestGenerateReturnsADifferentSecretEveryTime(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatal(err)
	}

	second, err := Generate()
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Error("expected two different secrets")
	}

	if len(first) < 32 {
		t.Errorf("expected a secret of at least 32 characters, got %d", len(first))
	}
}

// The comparison does not accept a prefix of the secret.
func TestEqualComparesTheWholeSecret(t *testing.T) {
	if !Equal("s3cret", "s3cret") {
		t.Error("expected the same secret to be equal")
	}

	if Equal("s3cre", "s3cret") {
		t.Error("expected a prefix not to be equal")
	}

	if Equal("", "s3cret") {
		t.Error("expected an empty secret not to be equal")
	}
}
