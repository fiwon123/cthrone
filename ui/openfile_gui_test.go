//go:build gui

package ui

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// On a phone Open serves the one file on loopback and points the browser at it:
// the server answers with the bytes, the type and the name, and the path
// without the token is not a way in (#470).
func TestOpenOnAndroidServesTheFileToTheBrowser(t *testing.T) {
	g := newScreenGUI(t, false)

	previous := opensInBrowser
	opensInBrowser = func() bool { return true }
	t.Cleanup(func() { opensInBrowser = previous })
	t.Cleanup(g.closeOpenServer)

	file := filepath.Join(t.TempDir(), "manual.pdf")
	body := []byte("%PDF-1.4 the whole file")

	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}

	opened := withOpener(t)

	g.openFile(file, "manual.pdf")

	if len(opened.opened) != 1 {
		t.Fatalf("Open asked for %d address(es), want one", len(opened.opened))
	}

	link := opened.opened[0]
	if !strings.HasPrefix(link, "http://127.0.0.1:") {
		t.Fatalf("Open asked for %q, want a loopback URL", link)
	}

	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(link)
	if err != nil {
		t.Fatalf("the browser could not fetch the file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/pdf") {
		t.Errorf("Content-Type = %q, want a PDF", got)
	}

	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "manual.pdf") {
		t.Errorf("Content-Disposition = %q, want the file's name", got)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("served %q, want %q", got, body)
	}

	// The token is the path; without it the server has nothing to serve.
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	parsed.Path = "/"

	denied, err := client.Get(parsed.String())
	if err != nil {
		t.Fatalf("fetching the base URL: %v", err)
	}
	defer denied.Body.Close()

	if denied.StatusCode != http.StatusNotFound {
		t.Errorf("a request without the token answered %d, want 404", denied.StatusCode)
	}
}

// A desktop still hands the file to the system opener, unchanged (#470).
func TestOpenOnADesktopUsesTheFileURI(t *testing.T) {
	g := newScreenGUI(t, false)

	previous := opensInBrowser
	opensInBrowser = func() bool { return false }
	t.Cleanup(func() { opensInBrowser = previous })

	file := filepath.Join(t.TempDir(), "manual.pdf")
	if err := os.WriteFile(file, []byte("%PDF-1.4"), 0o600); err != nil {
		t.Fatal(err)
	}

	opened := withOpener(t)

	g.openFile(file, "manual.pdf")

	if len(opened.opened) != 1 {
		t.Fatalf("Open asked for %d address(es), want one", len(opened.opened))
	}

	if got := opened.opened[0]; !strings.HasPrefix(got, "file://") || !strings.HasSuffix(got, "manual.pdf") {
		t.Errorf("Open asked for %q, want the file's own URI", got)
	}
}

// A name a peer chose cannot break the download header (#470).
func TestContentDispositionKeepsTheNameSafe(t *testing.T) {
	got := contentDisposition(`a"b\c` + "\r\nX-Evil: yes")

	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("Content-Disposition carries a line break: %q", got)
	}

	if !strings.Contains(got, "a_b_c") {
		t.Errorf("Content-Disposition = %q, want the quote and backslash replaced", got)
	}
}

// The token is 128 random bits, and two of them are not the same (#470).
func TestOpenTokensAreUnique(t *testing.T) {
	first, err := openToken()
	if err != nil {
		t.Fatal(err)
	}

	second, err := openToken()
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != 32 {
		t.Errorf("token length = %d, want 32 hex characters", len(first))
	}

	if first == second {
		t.Error("two openings share a token")
	}
}
