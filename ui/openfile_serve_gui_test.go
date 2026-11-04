//go:build gui

package ui

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// presentAPhone makes this build behave like the APK: Open serves the file to
// the browser on loopback instead of handing it to the system opener (#470).
func presentAPhone(t *testing.T) {
	t.Helper()

	previous := opensInBrowser
	opensInBrowser = func() bool { return true }

	t.Cleanup(func() { opensInBrowser = previous })
}

// tempFile writes body into dir under name and returns the path.
func tempFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// fetchOpenLink gets link once. A refusal from the listener is an error, which
// is how "the server is gone" is told apart from "the server said no".
func fetchOpenLink(link string) (int, string, error) {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(link)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}

	return resp.StatusCode, string(body), nil
}

// A second Open replaces the first: the old listener stops rather than leaving
// a second private file reachable, exactly as the comment on setOpenServer
// promises (#470, #485).
func TestASecondOpenStopsServingTheFirstFile(t *testing.T) {
	g := newScreenGUI(t, false)
	t.Cleanup(g.closeOpenServer)

	presentAPhone(t)
	opened := withOpener(t)

	dir := t.TempDir()
	first := tempFile(t, dir, "first.pdf", "the first file")
	second := tempFile(t, dir, "second.png", "the second file")

	g.openFile(first, "first.pdf")
	g.openFile(second, "second.png")

	if len(opened.opened) != 2 {
		t.Fatalf("Open asked for %d address(es), want two", len(opened.opened))
	}

	if status, _, err := fetchOpenLink(opened.opened[0]); err == nil {
		t.Errorf("the first link still answered (status %d) after a second Open", status)
	}

	status, body, err := fetchOpenLink(opened.opened[1])
	if err != nil {
		t.Fatalf("the second link did not answer: %v", err)
	}

	if status != http.StatusOK {
		t.Errorf("the second link answered %d, want 200", status)
	}

	if body != "the second file" {
		t.Errorf("the second link served %q, want the second file's bytes", body)
	}
}

// The link is bounded: after openServeWindow the server closes itself, so a
// forgotten Open cannot keep a private file reachable (#470, #485).
func TestAnOpenLinkStopsAfterItsWindow(t *testing.T) {
	g := newScreenGUI(t, false)
	t.Cleanup(g.closeOpenServer)

	presentAPhone(t)
	opened := withOpener(t)

	previous := openServeWindow
	openServeWindow = 50 * time.Millisecond
	t.Cleanup(func() { openServeWindow = previous })

	file := tempFile(t, t.TempDir(), "manual.pdf", "the file")
	g.openFile(file, "manual.pdf")

	link := opened.opened[0]

	if _, _, err := fetchOpenLink(link); err != nil {
		t.Fatalf("the link did not answer inside its own window: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)

	for {
		if _, _, err := fetchOpenLink(link); err != nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("the link still answered two seconds after a 50ms window")
		}

		time.Sleep(20 * time.Millisecond)
	}

	g.mu.Lock()
	live := g.openServer
	g.mu.Unlock()

	if live != nil {
		t.Error("the one-file server was left live after its window")
	}
}

// A file removed between the Open and the browser's fetch is reported as gone
// rather than served from a stale descriptor (#470, #485).
func TestAnOpenLinkSaysWhenTheFileIsGone(t *testing.T) {
	g := newScreenGUI(t, false)
	t.Cleanup(g.closeOpenServer)

	presentAPhone(t)
	opened := withOpener(t)

	file := tempFile(t, t.TempDir(), "manual.pdf", "the file")
	g.openFile(file, "manual.pdf")

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}

	status, body, err := fetchOpenLink(opened.opened[0])
	if err != nil {
		t.Fatalf("the link stopped answering instead of reporting the file: %v", err)
	}

	if status != http.StatusNotFound {
		t.Errorf("a removed file answered %d, want 404", status)
	}

	if !strings.Contains(body, "no longer there") {
		t.Errorf("a removed file said %q, want the sentence that says it is gone", body)
	}
}

// Closing the window stops the listener: the comment on shutdown says a
// listener left behind keeps a private file reachable after the app is gone
// (#470, #485).
func TestClosingTheWindowStopsTheOpenLink(t *testing.T) {
	g := newScreenGUI(t, false)
	t.Cleanup(g.closeOpenServer)

	presentAPhone(t)
	opened := withOpener(t)

	file := tempFile(t, t.TempDir(), "manual.pdf", "the file")
	g.openFile(file, "manual.pdf")

	link := opened.opened[0]

	if _, _, err := fetchOpenLink(link); err != nil {
		t.Fatalf("the link did not answer while the window was open: %v", err)
	}

	g.shutdown()

	if status, _, err := fetchOpenLink(link); err == nil {
		t.Errorf("the link still answered (status %d) after the window closed", status)
	}
}
