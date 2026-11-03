//go:build gui

package ui

import (
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// A folder cannot leave a phone for the same reason a file cannot, and a folder
// has no browser route, so the Files dialog offers no Open folder there (#474).
func TestCanOpenFolderOn(t *testing.T) {
	tests := map[string]bool{
		"linux":   true,
		"windows": true,
		"darwin":  true,
		"android": false,
		"ios":     false,
	}

	for goos, want := range tests {
		if got := canOpenFolderOn(goos); got != want {
			t.Errorf("canOpenFolderOn(%q) = %v, want %v", goos, got, want)
		}
	}
}

// The Files dialog lists what arrived in this conversation, and says where each
// file is: a saved one names its folder, an unsaved one says it is still in the
// app (#469).
func TestReceivedFilesListsTheConversationArrivals(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{
		{text: "hello"},
		{from: "laptop", file: &fileNote{name: "photo.png", size: 10, state: transferSaved, path: "/files/photo.png"}},
		{from: "phone", file: &fileNote{name: "notes.txt", size: 20, state: transferPending, path: "/staging/notes.txt"}},
		{from: "phone", file: &fileNote{name: "half.bin", size: 30, state: transferSending}},
		{file: &fileNote{name: "mine.txt", size: 40, state: transferSent, path: "/home/me/mine.txt"}},
	}
	g.mu.Unlock()

	got := g.receivedFiles()

	if len(got) != 2 {
		t.Fatalf("receivedFiles() returned %d file(s), want the two arrivals", len(got))
	}

	if !strings.Contains(got[0].line, "saved to /files/photo.png") {
		t.Errorf("the saved row reads %q, want the folder it landed in", got[0].line)
	}

	if !strings.Contains(got[1].line, "not saved (kept in the app)") {
		t.Errorf("the unsaved row reads %q, want where it is waiting", got[1].line)
	}

	if got[1].path != "/staging/notes.txt" {
		t.Errorf("the unsaved row's path is %q, want the staged file", got[1].path)
	}
}

// The automatic-save choice survives a restart, and turning it off goes back to
// the manual default (#469).
func TestStoreFileSettingsTogglesAutomaticSaving(t *testing.T) {
	g := newScreenGUI(t, false)
	folder := t.TempDir()

	g.storeFileSettings(folder, true)

	if got := g.fileSettings(); got.policy != saveAuto || got.folder != folder {
		t.Fatalf("settings after enabling = %+v, want auto in %s", got, folder)
	}

	g.storeFileSettings(folder, false)

	if got := g.fileSettings(); got.policy != saveManual {
		t.Errorf("settings after disabling = %+v, want manual", got)
	}
}

// The Files dialog opens with the current choice, and its Save button keeps it.
// The overlay is what the toolkit puts over the window; removing it is the
// dismiss a test can do without a pointer.
func TestFilesDialogOpens(t *testing.T) {
	g := newScreenGUI(t, false)

	g.showFiles()

	top := g.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("Files opened no dialog")
	}

	g.win.Canvas().Overlays().Remove(top)
}

// A file that is waiting to be saved is still on this disk, so a desktop can
// open it: a received PDF does not have to be saved before it is read (#469).
func TestAPendingFileCanBeOpenedOnADesktop(t *testing.T) {
	dir := t.TempDir()
	pdf := dir + "/manual.pdf"

	if err := os.WriteFile(pdf, []byte("%PDF-1.4 not really"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)
	recorder := withOpener(t)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", file: &fileNote{
		name:  "manual.pdf",
		size:  18,
		state: transferPending,
		path:  pdf,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, _, _, save, open := chatRowObjects(t, row)

	if !save.Visible() {
		t.Error("a pending PDF does not offer Save as…")
	}

	if !open.Visible() {
		t.Fatal("a pending PDF offers no way to open it")
	}

	test.Tap(open)

	if len(recorder.opened) != 1 {
		t.Fatalf("Open asked for %d addresses, want one", len(recorder.opened))
	}
}
