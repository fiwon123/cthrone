package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

// A release document is a public JSON file; every other answer — a 404, a rate
// limit, a body that is not JSON, a release with no tag — has to become an
// error rather than a release nobody can install.
func TestFetch(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantTag string
		wantErr string
	}{
		{
			name:   "a published release",
			status: http.StatusOK,
			body: `{"tag_name":"v0.10.0","assets":[
				{"name":"cthrone_v0.10.0_linux.tar.gz","browser_download_url":"https://example.invalid/a.tar.gz","size":10},
				{"name":"checksums.txt","browser_download_url":"https://example.invalid/checksums.txt","size":100}]}`,
			wantTag: "v0.10.0",
		},
		{name: "a missing release", status: http.StatusNotFound, body: `{"message":"Not Found"}`, wantErr: "404"},
		{name: "a body that is not JSON", status: http.StatusOK, body: "<html>no</html>", wantErr: "JSON"},
		{name: "a release with no tag", status: http.StatusOK, body: `{"assets":[]}`, wantErr: "named no version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			release, err := Fetch(context.Background(), server.Client(), server.URL)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Fetch() error = %v, want it to mention %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}

			if release.Tag != tt.wantTag {
				t.Errorf("Fetch() tag = %q, want %q", release.Tag, tt.wantTag)
			}

			if _, ok := release.Asset(ChecksumsName); !ok {
				t.Error("the release's checksums.txt was not kept")
			}
		})
	}
}

// The check is the same one a user would run by hand, and it has to refuse a
// download that is not the file the project published — that is the whole
// reason it runs before anything is replaced.
func TestVerify(t *testing.T) {
	archive := []byte("the archive bytes")
	sum := sha256.Sum256(archive)
	good := hex.EncodeToString(sum[:])

	tests := []struct {
		name      string
		checksums string
		wantErr   error
	}{
		{"the published hash", good + "  cthrone.tar.gz\n", nil},
		{"sha256sum's binary mark", good + " *cthrone.tar.gz\n", nil},
		{"an upper-case hash", strings.ToUpper(good) + "  cthrone.tar.gz\n", nil},
		{"a different hash", strings.Repeat("0", 64) + "  cthrone.tar.gz\n", ErrChecksum},
		{"the file is not listed", good + "  something-else.tar.gz\n", ErrChecksum},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify([]byte(tt.checksums), "cthrone.tar.gz", archive)

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Verify() error = %v", err)
				}

				return
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// Verify must say which two hashes disagreed: "the download is bad" leaves a
// user with nothing to compare.
func TestVerifyNamesBothHashes(t *testing.T) {
	err := Verify([]byte(strings.Repeat("a", 64)+"  x.tar.gz\n"), "x.tar.gz", []byte("other"))
	if err == nil {
		t.Fatal("Verify() accepted a wrong hash")
	}

	if !strings.Contains(err.Error(), strings.Repeat("a", 64)) || !strings.Contains(err.Error(), "is ") {
		t.Errorf("Verify() error = %v, want both hashes in it", err)
	}
}

// The CLI archive is flat and a tar.gz; the Windows one is a zip. Both are
// unpacked by name, and a member that is not there is an error rather than a
// zero-byte binary.
func TestBinary(t *testing.T) {
	binary := []byte("#!/bin/sh\necho cthrone\n")

	tarball := tarGz(t, map[string][]byte{"cthrone": binary, "ct": []byte("short"), "README.md": []byte("docs")})
	zipped := zipFile(t, map[string][]byte{"cthrone.exe": binary, "./ct": []byte("short")})

	tests := []struct {
		name    string
		archive []byte
		format  string
		member  string
		want    []byte
		wantErr string
	}{
		{"the command line binary out of a tar.gz", tarball, "x.tar.gz", "cthrone", binary, ""},
		{"a member that is not there", tarball, "x.tar.gz", "cthrone_gui", nil, "holds no cthrone_gui"},
		{"the zip's member, with a ./ prefix", zipped, "x.zip", "ct", []byte("short"), ""},
		{"the zip's .exe", zipped, "x.zip", "cthrone.exe", binary, ""},
		{"not an archive at all", []byte("hello"), "x.tar.gz", "cthrone", nil, "gzip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Binary(tt.archive, tt.format, tt.member)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Binary() error = %v, want it to mention %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Binary() error = %v", err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("Binary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The way back is the same swap in the other direction, and it works twice: an
// install keeps what it replaced, so reverting is a toggle rather than a
// one-way trip.
func TestInstallThenRevertToggles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cthrone")
	previous := target + PreviousSuffix

	writeFile(t, target, "version one", 0o755)

	kept, err := Install(target, []byte("version two"))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if kept != previous {
		t.Errorf("Install() kept %q, want %q", kept, previous)
	}

	assertFile(t, target, "version two")
	assertFile(t, previous, "version one")

	if _, err := Revert(target); err != nil {
		t.Fatalf("Revert() error = %v", err)
	}

	assertFile(t, target, "version one")
	assertFile(t, previous, "version two")

	if _, err := Revert(target); err != nil {
		t.Fatalf("the second Revert() error = %v", err)
	}

	assertFile(t, target, "version two")
}

// An install keeps the permissions of the binary it replaces: a binary the user
// or their package manager made 0755 or 0700 must not come back as something
// else.
func TestInstallKeepsTheMode(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cthrone")
	writeFile(t, target, "old", 0o700)

	if _, err := Install(target, []byte("new")); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("the installed binary is %v, want 0700: the mode was not kept", got)
	}
}

// A failure leaves the binary that was running exactly as it was, and leaves no
// half-installed file beside it.
func TestInstallLeavesEverythingOnAFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cthrone")

	// A directory where the binary should be: the rename cannot replace it.
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(target, []byte("new")); err == nil {
		t.Fatal("Install() replaced a directory, want a failure")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+filepath.Base(target)+".new-") {
			t.Errorf("a failed install left %q behind", entry.Name())
		}
	}
}

func TestInstallRefusesAnEmptyBinary(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cthrone")

	if _, err := Install(target, nil); err == nil {
		t.Fatal("Install() accepted an empty binary")
	}
}

func TestRevertWithoutAPrevious(t *testing.T) {
	if _, err := Revert(filepath.Join(t.TempDir(), "cthrone")); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("Revert() error = %v, want ErrNoPrevious", err)
	}
}

// A download that does not match the release's checksums is refused before
// anything is replaced: this is the guard against a truncated file, a captive
// portal's HTML, or a mirror nobody chose.
func TestApplyReleaseRefusesABadChecksum(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cthrone")
	writeFile(t, target, "the running binary", 0o755)

	archive := tarGz(t, map[string][]byte{"cthrone": []byte("the new binary")})
	release, _ := serveRelease(t, "v0.10.0", "cthrone_v0.10.0_linux.tar.gz", archive, false)

	_, err := ApplyRelease(context.Background(), http.DefaultClient, release, "cthrone_v0.10.0_linux.tar.gz", "cthrone", target)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("ApplyRelease() error = %v, want ErrChecksum", err)
	}

	assertFile(t, target, "the running binary")
}

// The whole path with a release the test serves itself: fetch, verify, unpack,
// swap, and the previous binary beside it.
func TestRunInstallsAVerifiedRelease(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cthrone")
	previous := target + PreviousSuffix
	writeFile(t, target, "the running binary", 0o755)

	asset, ok := updatepkg.ArchiveName("v0.10.0", false, runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("this host has no archive in the release matrix")
	}

	archive := tarGz(t, map[string][]byte{"cthrone": []byte("the new binary")})
	server, _ := serveReleaseServer(t, "v0.10.0", asset, archive, true)

	result, err := Run(context.Background(), server.Client(), server.URL, Request{
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		Target: target,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Tag != "v0.10.0" || result.Asset != asset {
		t.Errorf("Run() result = %+v, want the v0.10.0 release and %q", result, asset)
	}

	if result.Previous != previous {
		t.Errorf("Run() kept %q, want %q", result.Previous, previous)
	}

	// The reported checksum is the archive's, as the release recorded it: the
	// one number a user can check with `sha256sum` against the asset. The
	// extracted binary's hash is a different value that nothing compared, so
	// reporting it under "verified" would be a claim about a check nobody made.
	archiveSum := sha256.Sum256(archive)
	wantArchive := hex.EncodeToString(archiveSum[:])
	binarySum := sha256.Sum256([]byte("the new binary"))

	if result.Checksum != wantArchive {
		t.Errorf("Run() reported %q, want the archive's checksum %q", result.Checksum, wantArchive)
	}

	if result.Checksum == hex.EncodeToString(binarySum[:]) {
		t.Error("Run() reported the extracted binary's checksum, which was never verified")
	}

	assertFile(t, target, "the new binary")
	assertFile(t, previous, "the running binary")
}

// A release that publishes nothing for this platform is a refusal, not a guess
// at a nearby file name.
func TestRunRefusesAPlatformTheReleaseHasNoArchiveFor(t *testing.T) {
	server, _ := serveReleaseServer(t, "v0.10.0", "cthrone_v0.10.0_linux.tar.gz", tarGz(t, map[string][]byte{"cthrone": []byte("x")}), true)

	_, err := Run(context.Background(), server.Client(), server.URL, Request{
		GOOS:   "plan9",
		GOARCH: "mips",
		Target: filepath.Join(t.TempDir(), "cthrone"),
	})
	if !errors.Is(err, ErrNoAsset) {
		t.Fatalf("Run() error = %v, want ErrNoAsset", err)
	}
}

// The self-install has to name the binary that is running, resolved through a
// symlink so the file is replaced and not the link.
func TestRunningBinary(t *testing.T) {
	path, err := RunningBinary()
	if err != nil {
		t.Fatalf("RunningBinary() error = %v", err)
	}

	if !filepath.IsAbs(path) {
		t.Errorf("RunningBinary() = %q, want an absolute path", path)
	}
}

func TestMember(t *testing.T) {
	if got := Member(false); got != "cthrone" {
		t.Errorf("Member(false) = %q, want the command line binary", got)
	}

	if got := Member(true); got != "cthrone_gui" {
		t.Errorf("Member(true) = %q, want the window binary", got)
	}
}

// --- helpers -------------------------------------------------------------

// tarGz builds a flat tar.gz with the members given, the way `make release`
// does.
func tarGz(t *testing.T, members map[string][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	zipped := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(zipped)

	for name, content := range members {
		if err := archive.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := archive.Write(content); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// zipFile builds a zip with the members given, the way the Windows archive is.
func zipFile(t *testing.T, members map[string][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	archive := zip.NewWriter(&buffer)

	for name, content := range members {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// serveRelease builds a release document pointing at a server that serves one
// archive and its checksums, with the option of publishing a wrong checksum.
func serveRelease(t *testing.T, tag, asset string, archive []byte, honest bool) (*Release, []byte) {
	t.Helper()

	server, sums := serveReleaseServer(t, tag, asset, archive, honest)

	return documentFor(t, server.URL, tag, asset), sums
}

func serveReleaseServer(t *testing.T, tag, asset string, archive []byte, honest bool) (*httptest.Server, []byte) {
	t.Helper()

	sum := sha256.Sum256(archive)
	hash := hex.EncodeToString(sum[:])

	if !honest {
		hash = strings.Repeat("0", 64)
	}

	sums := []byte(hash + "  " + asset + "\n")

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+ChecksumsName):
			_, _ = w.Write(sums)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(archive)
		case strings.Contains(r.URL.Path, "/releases/"):
			_, _ = w.Write([]byte(releaseDocument(tag, server.URL, asset, len(archive), len(sums))))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server, sums
}

// documentFor is a release whose asset URLs point at an httptest server.
func documentFor(t *testing.T, base, tag, asset string) *Release {
	t.Helper()

	return &Release{Tag: tag, Assets: []Asset{
		{Name: asset, URL: base + "/download/" + asset},
		{Name: ChecksumsName, URL: base + "/download/" + ChecksumsName},
	}}
}

func releaseDocument(tag, base, asset string, archiveSize, sumsSize int) string {
	return `{"tag_name":"` + tag + `","assets":[` +
		`{"name":"` + asset + `","browser_download_url":"` + base + `/download/` + asset + `","size":` + strconv.Itoa(archiveSize) + `},` +
		`{"name":"` + ChecksumsName + `","browser_download_url":"` + base + `/download/` + ChecksumsName + `","size":` + strconv.Itoa(sumsSize) + `}]}`
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Base(path), err)
	}

	if string(got) != want {
		t.Errorf("%s = %q, want %q", filepath.Base(path), got, want)
	}
}
