package install

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The list is the record of what was published, in the order it came out, and
// `PreviousTag` reads it rather than doing version arithmetic: v0.12.1 → v0.12.0
// today, and whatever the nightlies look like tomorrow (#384).
func TestPreviousTagFollowsThePublishedOrder(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		current string
		want    string
	}{
		{"the release before the newest", []string{"v0.12.1", "v0.12.0", "v0.11.7"}, "v0.12.1", "v0.12.0"},
		{"from the middle", []string{"v0.12.1", "v0.12.0", "v0.11.7"}, "v0.12.0", "v0.11.7"},
		{"the oldest has nothing before it", []string{"v0.12.1", "v0.12.0"}, "v0.12.0", ""},
		{"a build that was never published", []string{"v0.12.1", "v0.12.0"}, "vx.x.x", ""},
		{"an empty list", nil, "v0.12.1", ""},
		{"a release published out of order", []string{"v0.11.0", "v0.10.0"}, "v0.11.0", "v0.10.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PreviousTag(tt.tags, tt.current); got != tt.want {
				t.Errorf("PreviousTag(%v, %q) = %q, want %q", tt.tags, tt.current, got, tt.want)
			}
		})
	}
}

// ReleaseTags reads the tags out of the list GitHub serves, newest first, and
// leaves a draft out of it.
func TestReleaseTagsReadsTheList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.12.1", "draft": false},
			{"tag_name": "v0.12.0", "draft": false},
			{"tag_name": "v0.13.0", "draft": true},
			{"tag_name": "", "draft": false},
		})
	}))
	defer server.Close()

	tags, err := ReleaseTags(context.Background(), server.Client(), ReleasesURL(server.URL))
	if err != nil {
		t.Fatalf("ReleaseTags() error = %v", err)
	}

	want := []string{"v0.12.1", "v0.12.0"}

	if len(tags) != len(want) {
		t.Fatalf("ReleaseTags() = %v, want %v", tags, want)
	}

	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tag %d = %q, want %q", i, tags[i], want[i])
		}
	}
}

// An answer that is not the list is an error, not an empty list: "no previous
// release" and "the list could not be read" are different answers.
func TestReleaseTagsRefusesAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := ReleaseTags(context.Background(), server.Client(), ReleasesURL(server.URL)); err == nil {
		t.Fatal("ReleaseTags() accepted a 403")
	}
}
