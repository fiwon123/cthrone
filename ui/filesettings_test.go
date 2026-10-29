package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// memoryStore is a preference store that lives in a map: the rules are what is
// under test, and a toolkit is what would prove the widget.
type memoryStore struct {
	strings map[string]string
	bools   map[string]bool
	writes  int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{strings: map[string]string{}, bools: map[string]bool{}}
}

func (s *memoryStore) String(key string) string { return s.strings[key] }
func (s *memoryStore) Bool(key string) bool     { return s.bools[key] }

func (s *memoryStore) SetString(key, value string) {
	s.strings[key] = value
	s.writes++
}

func (s *memoryStore) SetBool(key string, value bool) {
	s.bools[key] = value
	s.writes++
}

// TestReadFileSettingsDefaults is a fresh install: the platform's folder and no
// dialog, because a queue of files must not become a queue of dialogs.
func TestReadFileSettingsDefaults(t *testing.T) {
	settings := readFileSettings(newMemoryStore(), "/home/me/Downloads")

	if settings.folder != "/home/me/Downloads" {
		t.Errorf("folder = %q, want the fallback", settings.folder)
	}

	if settings.policy != saveAuto {
		t.Errorf("policy = %v, want auto", settings.policy)
	}
}

// TestReadFileSettingsStored is the choice surviving a restart: what was written
// is what is read back.
func TestReadFileSettingsStored(t *testing.T) {
	store := newMemoryStore()

	writeFileSettings(store, fileSettings{folder: "/data/files", policy: saveAsk})

	got := readFileSettings(store, "/home/me/Downloads")
	if got.folder != "/data/files" {
		t.Errorf("folder = %q, want /data/files", got.folder)
	}

	if got.policy != saveAsk {
		t.Errorf("policy = %v, want ask", got.policy)
	}

	// Switching back must store the zero value, not leave "ask" behind.
	writeFileSettings(store, fileSettings{folder: "/data/files", policy: saveAuto})

	if got := readFileSettings(store, "/home/me/Downloads"); got.policy != saveAuto {
		t.Errorf("policy after asking was turned off = %v, want auto", got.policy)
	}
}

// TestReadFileSettingsIgnoresARelativeFolder keeps a path that would resolve
// against the launcher's working directory from being obeyed.
func TestReadFileSettingsIgnoresARelativeFolder(t *testing.T) {
	store := newMemoryStore()
	store.SetString(prefsDownloadFolder, "files/downloads")

	if got := readFileSettings(store, "/home/me/Downloads"); got.folder != "/home/me/Downloads" {
		t.Errorf("folder = %q, want the fallback rather than a relative path", got.folder)
	}
}

// TestReadFileSettingsNilStore is the no-toolkit case: a window that has no
// preferences still saves somewhere.
func TestReadFileSettingsNilStore(t *testing.T) {
	got := readFileSettings(nil, "/home/me/Downloads")
	if got.folder != "/home/me/Downloads" || got.policy != saveAuto {
		t.Errorf("settings = %+v, want the fallback and auto", got)
	}

	writeFileSettings(nil, fileSettings{folder: "/x", policy: saveAsk})
}

// TestExpandFolder covers what a user actually types and what a picker hands
// over.
func TestExpandFolder(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"absolute", "/data/files", "/data/files"},
		{"trailing slash", "/data/files/", "/data/files"},
		{"spaces", "  /data/files  ", "/data/files"},
		{"empty", "", ""},
		{"relative", "data/files", ""},
		{"dots", "../../etc", ""},
		{"home", "~", home},
		{"home child", "~/Downloads", filepath.Join(home, "Downloads")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandFolder(tt.in); got != tt.want {
				t.Errorf("expandFolder(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNumberedName pins the collision rule: the assembler's, so a name that
// arrives twice is resolved the same way wherever it is noticed.
func TestNumberedName(t *testing.T) {
	tests := []struct {
		i    int
		want string
	}{
		{0, "photo.jpg"},
		{1, "photo (1).jpg"},
		{2, "photo (2).jpg"},
	}

	for _, tt := range tests {
		if got := numberedName("photo.jpg", tt.i); got != tt.want {
			t.Errorf("numberedName(photo.jpg, %d) = %q, want %q", tt.i, got, tt.want)
		}
	}

	if got := numberedName("archive.tar.gz", 1); got != "archive.tar (1).gz" {
		t.Errorf("numberedName(archive.tar.gz, 1) = %q, want archive.tar (1).gz", got)
	}

	if got := numberedName("README", 1); got != "README (1)" {
		t.Errorf("numberedName(README, 1) = %q, want README (1)", got)
	}
}

// TestUniquePath is the acceptance criterion that two transfers with the same
// name both land: the second one is renamed, never written over the first.
func TestUniquePath(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := uniquePath(dir, "photo.jpg")
	if err != nil {
		t.Fatalf("uniquePath() error = %v", err)
	}

	if want := filepath.Join(dir, "photo (1).jpg"); got != want {
		t.Errorf("uniquePath() = %q, want %q", got, want)
	}

	// A free name is used as it is.
	if got, err := uniquePath(dir, "other.jpg"); err != nil || got != filepath.Join(dir, "other.jpg") {
		t.Errorf("uniquePath(other.jpg) = %q, %v", got, err)
	}

	// The file that was there is untouched.
	data, err := os.ReadFile(filepath.Join(dir, "photo.jpg"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "first" {
		t.Errorf("the existing file holds %q, want it untouched", data)
	}
}

// TestUniquePathGivesUp stops counting rather than looping over a folder that
// already holds every name it would try.
func TestUniquePathGivesUp(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i <= maxSuffixes; i++ {
		name := numberedName("x.bin", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := uniquePath(dir, "x.bin"); err == nil {
		t.Fatal("uniquePath() found a name in a folder that holds every one of them")
	}
}

// TestMoveInto moves a staged file into the folder and reports where it landed.
func TestMoveInto(t *testing.T) {
	staging := t.TempDir()
	downloads := filepath.Join(t.TempDir(), "Downloads")

	staged := filepath.Join(staging, "notes.txt")
	if err := os.WriteFile(staged, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := moveInto(staged, downloads, "notes.txt")
	if err != nil {
		t.Fatalf("moveInto() error = %v", err)
	}

	if want := filepath.Join(downloads, "notes.txt"); got != want {
		t.Errorf("moveInto() = %q, want %q", got, want)
	}

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("the staged copy is still there: %v", err)
	}

	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "hello" {
		t.Errorf("moved file holds %q, want hello", data)
	}
}

// TestMoveIntoDoesNotOverwrite is the same acceptance criterion one level down:
// a file already in the folder keeps its bytes.
func TestMoveIntoDoesNotOverwrite(t *testing.T) {
	staging := t.TempDir()
	downloads := t.TempDir()

	existing := filepath.Join(downloads, "photo.jpg")
	if err := os.WriteFile(existing, []byte("earlier session"), 0o600); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(staging, "photo.jpg")
	if err := os.WriteFile(staged, []byte("new arrival"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := moveInto(staged, downloads, "photo.jpg")
	if err != nil {
		t.Fatalf("moveInto() error = %v", err)
	}

	if want := filepath.Join(downloads, "photo (1).jpg"); got != want {
		t.Errorf("moveInto() = %q, want %q", got, want)
	}

	old, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}

	if string(old) != "earlier session" {
		t.Errorf("the earlier file now holds %q, want it untouched", old)
	}

	fresh, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}

	if string(fresh) != "new arrival" {
		t.Errorf("the new file holds %q, want the arrival", fresh)
	}
}

// TestMoveIntoMissingSource is a transfer whose staged file was swept away: the
// caller must be told, not handed an empty file.
func TestMoveIntoMissingSource(t *testing.T) {
	dir := t.TempDir()

	if _, err := moveInto(filepath.Join(t.TempDir(), "gone.bin"), dir, "gone.bin"); err == nil {
		t.Fatal("moveInto() accepted a source that does not exist")
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 0 {
		t.Errorf("moveInto() left %d files in the folder after failing", len(items))
	}
}

// TestDefaultStagingDir keeps the staging folder inside the app's storage, and
// out of the download folder the user will look in.
func TestDefaultStagingDir(t *testing.T) {
	if got, want := defaultStagingDir("/data/app"), filepath.Join("/data/app", "incoming"); got != want {
		t.Errorf("defaultStagingDir(/data/app) = %q, want %q", got, want)
	}

	if got := defaultStagingDir(""); got == "" {
		t.Error("defaultStagingDir(\"\") is empty")
	}
}

// TestDefaultDownloadIsNotTheSecretDirectory is the rule from AGENTS.md checked
// against a throwaway home: the folder a fresh install saves into must not be
// the directory that holds the token and the TLS key.
func TestDefaultDownloadIsNotTheSecretDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	folder := resolveDownloadDir(false, "")

	secret := filepath.Join(home, ".config", "cthrone")
	if folder == secret || filepath.Dir(folder) == secret {
		t.Fatalf("the download folder %s is the secret directory", folder)
	}

	if want := filepath.Join(home, "Downloads"); folder != want {
		t.Errorf("download folder = %q, want %q", folder, want)
	}
}

// TestCopyInto is the ask-mode write: the verified bytes reach the writer the
// save dialog handed over.
func TestCopyInto(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "photo.jpg")

	if err := os.WriteFile(source, []byte("the verified bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := copyInto(&out, source); err != nil {
		t.Fatalf("copyInto() error = %v", err)
	}

	if got, want := out.String(), "the verified bytes"; got != want {
		t.Errorf("copied %q, want %q", got, want)
	}

	// The source is still there: a copy is not a move, and a failed write must
	// leave the original where it was.
	if _, err := os.Stat(source); err != nil {
		t.Errorf("the source is gone: %v", err)
	}
}

// TestCopyIntoMissingSource is a staged file that a sweep removed first.
func TestCopyIntoMissingSource(t *testing.T) {
	var out bytes.Buffer

	if err := copyInto(&out, filepath.Join(t.TempDir(), "gone.bin")); err == nil {
		t.Fatal("copyInto() accepted a source that does not exist")
	}

	if out.Len() != 0 {
		t.Errorf("copyInto() wrote %d bytes from a missing source", out.Len())
	}
}
