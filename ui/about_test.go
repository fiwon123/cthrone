package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The update check reads one field out of GitHub's answer, and every other
// answer — a proxy error page, a rate-limit message, a document with no tag —
// has to become an error the tab can show instead of a version nobody can
// compare. No case here reaches the network: the server is a local one.
func TestLatestReleaseTag(t *testing.T) {
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
			wantErr: errNoReleaseVersion.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			got, err := latestReleaseTag(context.Background(), server.Client(), server.URL)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("latestReleaseTag() = %q, want the error containing %q", got, tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("latestReleaseTag() error = %v, want it to mention %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("latestReleaseTag() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("latestReleaseTag() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A server that never answers has to end the check, not hang the tab. The
// timeout is the whole reason the button can be pressed on a dead network.
func TestLatestReleaseTagTimesOut(t *testing.T) {
	release := make(chan struct{})

	// The handler waits for a release that only arrives at the end of the test,
	// but it also gives up when the client does: the client's timeout cancels
	// the request, and a handler that ignored that would make Server.Close
	// wait for a handler nobody can reach any more.
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

	_, err := latestReleaseTag(context.Background(), client, server.URL)
	if err == nil {
		t.Fatal("a server that never answers produced no error")
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the check took %v, want it bounded by the client timeout", elapsed)
	}
}

// A cancelled context ends the check too: the tab may be closed, and the call
// must not outlive the reason it was made.
func TestLatestReleaseTagHonoursTheContext(t *testing.T) {
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

	if _, err := latestReleaseTag(ctx, server.Client(), server.URL); !errors.Is(err, context.Canceled) {
		t.Errorf("latestReleaseTag() error = %v, want context.Canceled", err)
	}
}

// The sentence the tab shows has to distinguish "there is a newer release" from
// "this is the newest one", and it must not call a release newer than a build
// whose version it cannot read.
func TestUpdateText(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    string
	}{
		{
			name:    "a newer release",
			current: "v0.8.0",
			latest:  "v0.8.1",
			want:    "v0.8.1 is available",
		},
		{
			name:    "the same release",
			current: "v0.8.1",
			latest:  "v0.8.1",
			want:    "Up to date (v0.8.1)",
		},
		{
			name:    "an older release published last",
			current: "v0.8.2",
			latest:  "v0.8.1",
			want:    "Up to date (v0.8.2)",
		},
		{
			name:    "a git describe build",
			current: "v0.8.0-5-gabc123",
			latest:  "v0.8.1",
			want:    "v0.8.1 is available",
		},
		{
			name:    "a build with no version",
			current: placeholderVersion,
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
			got := updateText(tt.current, tt.latest)

			if !strings.Contains(got, tt.want) {
				t.Errorf("updateText(%q, %q) = %q, want it to contain %q", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// A version is the leading dotted numbers, and its trailing parts default to
// zero — the comparison a user makes, not a string comparison.
func TestCompareVersions(t *testing.T) {
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
			left, ok := parseVersion(tt.a)
			if !ok {
				t.Fatalf("parseVersion(%q) failed", tt.a)
			}

			right, ok := parseVersion(tt.b)
			if !ok {
				t.Fatalf("parseVersion(%q) failed", tt.b)
			}

			if got := compareVersions(left, right); got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// The injected version wins; without one the packager's metadata is the next
// answer, because that is how the APK is versioned. Fyne's "0.0.1" placeholder
// is not a version this program shipped.
func TestAboutVersion(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		metadata string
		want     string
	}{
		{"injected", "v0.8.1", "0.9.0", "v0.8.1"},
		{"the packager's version", placeholderVersion, "0.8.1", "v0.8.1"},
		{"the packager's version already prefixed", placeholderVersion, "v0.8.1", "v0.8.1"},
		{"no injected version, no metadata", placeholderVersion, "", placeholderVersion},
		{"Fyne's placeholder is not a version", placeholderVersion, zeroMetadataVersion, placeholderVersion},
		{"an empty injected value", "", "", placeholderVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aboutVersion(tt.injected, tt.metadata); got != tt.want {
				t.Errorf("aboutVersion(%q, %q) = %q, want %q", tt.injected, tt.metadata, got, tt.want)
			}
		})
	}
}
