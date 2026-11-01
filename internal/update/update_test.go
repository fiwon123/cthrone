package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The check reads one field out of GitHub's answer, and every other answer — a
// proxy error page, a rate-limit message, a document with no tag — has to become
// an error the caller can show instead of a version nobody can compare. No case
// here reaches the network: the server is a local one.
func TestLatestTag(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr string
	}{
		{
			name:   "a published release",
			status: http.StatusOK,
			body:   `{"tag_name":"v0.8.1","name":"v0.8.1","draft":false}`,
			want:   "v0.8.1",
		},
		{
			name:   "the tag is trimmed",
			status: http.StatusOK,
			body:   `{"tag_name":"  v0.9.0\n"}`,
			want:   "v0.9.0",
		},
		{
			name:    "a rate-limit answer",
			status:  http.StatusForbidden,
			body:    `{"message":"API rate limit exceeded"}`,
			wantErr: "403",
		},
		{
			name:    "a body that is not JSON",
			status:  http.StatusOK,
			body:    "<html>not a release</html>",
			wantErr: "JSON",
		},
		{
			name:    "a release with no tag",
			status:  http.StatusOK,
			body:    `{"name":"something"}`,
			wantErr: ErrNoReleaseVersion.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			got, err := LatestTag(context.Background(), server.Client(), server.URL)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LatestTag() = %q, want the error containing %q", got, tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("LatestTag() error = %v, want it to mention %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("LatestTag() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("LatestTag() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A server that never answers has to end the check, not hang the window. The
// timeout is the whole reason the button can be pressed on a dead network.
func TestLatestTagTimesOut(t *testing.T) {
	release := make(chan struct{})

	// The handler waits for a release that only arrives at the end of the test,
	// but it also gives up when the client does: the client's timeout cancels
	// the request, and a handler that ignored that would make Server.Close wait
	// for a handler nobody can reach any more.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	client := &http.Client{Timeout: 50 * time.Millisecond}

	start := time.Now()

	if _, err := LatestTag(context.Background(), client, server.URL); err == nil {
		t.Fatal("a server that never answers produced no error")
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the check took %v, want it bounded by the client timeout", elapsed)
	}
}

// A cancelled context ends the check too: the window may be closed, and the call
// must not outlive the reason it was made.
func TestLatestTagHonoursTheContext(t *testing.T) {
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := LatestTag(ctx, server.Client(), server.URL); !errors.Is(err, context.Canceled) {
		t.Errorf("LatestTag() error = %v, want context.Canceled", err)
	}
}

// The sentence a user reads has to distinguish "there is a newer release" from
// "this is the newest one", and it must not call a release newer than a build
// whose version it cannot read.
func TestText(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		archive string
		want    string
		absent  string
	}{
		{
			name:    "a newer release, with the archive named",
			current: "v0.8.0",
			latest:  "v0.8.1",
			archive: "cthrone_v0.8.1_linux.tar.gz",
			want:    "v0.8.1 is available — this build is v0.8.0. Download cthrone_v0.8.1_linux.tar.gz from the release page.",
		},
		{
			name:    "a newer release with no archive for this build",
			current: "v0.8.0",
			latest:  "v0.8.1",
			want:    "v0.8.1 is available — this build is v0.8.0. The release page has the archive for this platform.",
		},
		{
			name:    "the same release",
			current: "v0.8.1",
			latest:  "v0.8.1",
			want:    "Up to date (v0.8.1).",
		},
		{
			name:    "an older release published last",
			current: "v0.8.2",
			latest:  "v0.8.1",
			want:    "Up to date (v0.8.2).",
		},
		{
			name:    "a git describe build",
			current: "v0.8.0-5-gabc123",
			latest:  "v0.8.1",
			want:    "v0.8.1 is available — this build is v0.8.0-5-gabc123.",
		},
		{
			name:    "a build with no version",
			current: "vx.x.x",
			latest:  "v0.8.1",
			want:    "does not know its version; the latest release is v0.8.1",
		},
		{
			name:    "a release with no readable version",
			current: "v0.8.1",
			latest:  "nightly",
			want:    "names no version this app can read (nightly)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Text(tt.current, tt.latest, tt.archive)

			if !strings.Contains(got, tt.want) {
				t.Errorf("Text(%q, %q, %q) = %q, want it to contain %q", tt.current, tt.latest, tt.archive, got, tt.want)
			}

			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("Text(%q, %q, %q) = %q, want it not to contain %q", tt.current, tt.latest, tt.archive, got, tt.absent)
			}
		})
	}
}

// A build that cannot name its own version must never be told it is up to date:
// vx.x.x would compare as older than everything, so the sentence has to say the
// comparison could not be made.
func TestTextNeverCallsAnUnknownBuildCurrent(t *testing.T) {
	for _, current := range []string{"vx.x.x", "", "unknown", "dev"} {
		got := Text(current, "v0.10.0", "cthrone_v0.10.0_linux.tar.gz")

		if strings.Contains(got, "Up to date") {
			t.Errorf("Text(%q, ...) = %q, want it not to claim the build is current", current, got)
		}
	}
}

// A version is the leading dotted numbers, and its trailing parts default to
// zero — the comparison a user makes, not a string comparison.
func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.8.0", "v0.8.1", -1},
		{"v0.8.1", "v0.8.0", 1},
		{"v0.8.1", "0.8.1", 0},
		{"0.8", "0.8.0", 0},
		{"v0.10.0", "v0.9.9", 1},
		{"v1.0.0-rc1", "v1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			left, ok := Parse(tt.a)
			if !ok {
				t.Fatalf("Parse(%q) failed", tt.a)
			}

			right, ok := Parse(tt.b)
			if !ok {
				t.Fatalf("Parse(%q) failed", tt.b)
			}

			if got := Compare(left, right); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ArchiveName names the file the release actually publishes, per platform and
// per build kind. A name invented here is worse than none: it sends the user to
// a 404.
func TestArchiveName(t *testing.T) {
	tests := []struct {
		name   string
		gui    bool
		goos   string
		goarch string
		want   string
		ok     bool
	}{
		{"the Linux command line", false, "linux", "amd64", "cthrone_v0.10.0_linux.tar.gz", true},
		{"the Windows command line", false, "windows", "amd64", "cthrone_v0.10.0_windows.zip", true},
		{"the Android command line", false, "android", "arm64", "cthrone_v0.10.0_android.tar.gz", true},
		{"a macOS command line", false, "darwin", "arm64", "cthrone_v0.10.0_darwin_arm64.tar.gz", true},
		{"an Intel macOS command line", false, "darwin", "amd64", "cthrone_v0.10.0_darwin_amd64.tar.gz", true},
		{"the Linux window", true, "linux", "amd64", "cthrone_v0.10.0_linux_gui_amd64.tar.gz", true},
		{"the Windows window", true, "windows", "amd64", "cthrone_v0.10.0_windows_gui_amd64.zip", true},
		{"the macOS window", true, "darwin", "arm64", "cthrone_v0.10.0_darwin_gui_arm64.tar.gz", true},
		{"the APK", true, "android", "arm64", "cthrone_v0.10.0_android_gui_arm64.apk", true},
		{"a platform the release does not build for", true, "linux", "arm64", "", false},
		{"a system nobody releases for", false, "plan9", "mips", "", false},
		{"case does not decide it", false, "Linux", "AMD64", "cthrone_v0.10.0_linux.tar.gz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ArchiveName("v0.10.0", tt.gui, tt.goos, tt.goarch)

			if ok != tt.ok {
				t.Fatalf("ArchiveName(%v, %s/%s) ok = %v, want %v", tt.gui, tt.goos, tt.goarch, ok, tt.ok)
			}

			if got != tt.want {
				t.Errorf("ArchiveName(%v, %s/%s) = %q, want %q", tt.gui, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

// The names above are only right if the build actually writes those files, so
// check them against the Makefile: `make release` and `make release-gui` are
// where an archive name comes from. A rename there without a rename here would
// otherwise be found by a user, on a release page, with a 404.
func TestArchiveNamesMatchTheMakefile(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Skipf("no Makefile to check against: %v", err)
	}

	text := strings.ReplaceAll(string(makefile), "$(APP_NAME)", "cthrone")
	text = strings.ReplaceAll(text, "$(VERSION)", "v0.10.0")

	for _, build := range []struct {
		name   string
		gui    bool
		goos   string
		goarch string
	}{
		{"the Linux command line", false, "linux", "amd64"},
		{"the Windows command line", false, "windows", "amd64"},
		{"the Android command line", false, "android", "arm64"},
		{"a macOS command line", false, "darwin", "arm64"},
		{"an Intel macOS command line", false, "darwin", "amd64"},
		{"the Linux window", true, "linux", "amd64"},
		{"the Windows window", true, "windows", "amd64"},
		{"the macOS window", true, "darwin", "arm64"},
		{"the APK", true, "android", "arm64"},
	} {
		name, ok := ArchiveName("v0.10.0", build.gui, build.goos, build.goarch)
		if !ok {
			t.Fatalf("ArchiveName(%s) named nothing", build.name)
		}

		if !strings.Contains(text, name) {
			t.Errorf("the Makefile never builds %q, which ArchiveName names for %s", name, build.name)
		}
	}
}
