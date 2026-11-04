//go:build gui

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// rowSender finds the sender line a painted row carries.
func rowSender(t *testing.T, row fyne.CanvasObject) *widgets.Text {
	t.Helper()

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want *fyne.Container", row)
	}

	caption, ok := box.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("row[0] is %T, want the caption cell", box.Objects[0])
	}

	var sender *widgets.Text

	for _, object := range caption.Objects {
		part, ok := object.(*fyne.Container)
		if !ok {
			continue
		}

		for _, child := range part.Objects {
			if inner, ok := child.(*fyne.Container); ok {
				for _, item := range inner.Objects {
					if text, ok := item.(*widgets.Text); ok {
						sender = text
					}
				}
			}
		}
	}

	if sender == nil {
		t.Fatal("the caption cell holds no sender line")
	}

	return sender
}

// The range gesture is the one a thumb can make: press Select, tap the first
// message, tap the last, then copy. The header becomes the actions for that
// span, and the rows in it are marked (#469).
func TestSelectionCopiesARangeAndMarksIt(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{text: "one"}, {from: "laptop", text: "two"}, {text: "three"}}
	g.mu.Unlock()
	g.chatList.Refresh()

	g.beginSelection()

	if !g.sel.active {
		t.Fatal("Select did not enter range mode")
	}

	if !g.selectHeader.Visible() {
		t.Error("the selection header is not shown")
	}

	if g.headerHolder.Objects[0].Visible() {
		t.Error("the state header is still shown beside the selection actions")
	}

	g.tapSelection(0)
	g.tapSelection(2)

	if got := g.sel.count(); got != 3 {
		t.Fatalf("the span covers %d row(s), want 3", got)
	}

	row := g.chatList.CreateItem()
	g.updateChatRow(1, row)

	if sender := rowSender(t, row); !strings.HasPrefix(sender.Text(), "✓ ") {
		t.Errorf("a selected row's sender reads %q, want a check in front", sender.Text())
	}

	g.app.Clipboard().SetContent("")
	g.copySelection()

	want := transcriptOf(g.messages)
	if got := g.app.Clipboard().Content(); got != want {
		t.Errorf("Copy put %q on the clipboard, want %q", got, want)
	}

	if g.sel.active {
		t.Error("the copy left range mode active")
	}
}

// Deleting a span clears the rows on this device and the app's own unsaved copy
// of a file in it. A saved file and the user's own sent file are not this
// function's to remove (#469).
func TestDeletingASelectionKeepsFilesTheUserKeeps(t *testing.T) {
	g := newScreenGUI(t, false)

	staging := g.stagingDir()
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(staging, "waiting.txt")
	if err := os.WriteFile(staged, []byte("still here"), 0o600); err != nil {
		t.Fatal(err)
	}

	saved := filepath.Join(t.TempDir(), "kept.txt")
	if err := os.WriteFile(saved, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "mine.txt")
	if err := os.WriteFile(source, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	g.mu.Lock()
	g.messages = []chatMessage{
		{from: "laptop", file: &fileNote{name: "waiting.txt", path: staged, state: transferPending}},
		{from: "laptop", file: &fileNote{name: "kept.txt", path: saved, state: transferSaved}},
		{file: &fileNote{name: "mine.txt", path: source, state: transferSent}},
	}
	g.mu.Unlock()

	g.beginSelection()
	g.tapSelection(0)
	g.tapSelection(2)
	g.deleteSelection()

	if len(g.messages) != 0 {
		t.Errorf("deleting the span left %d row(s)", len(g.messages))
	}

	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Error("the app's unsaved copy survived the row that owned it")
	}

	if _, err := os.Stat(saved); err != nil {
		t.Errorf("a saved file was removed with its row: %v", err)
	}

	if _, err := os.Stat(source); err != nil {
		t.Errorf("the user's own sent file was removed with its row: %v", err)
	}
}

// Clear all is the "start clean" action: it asks first, then empties the
// transcript and the app's unsaved copies, and leaves saved files alone (#469).
func TestClearAllEmptiesTheTranscriptAndKeepsSavedFiles(t *testing.T) {
	g := newScreenGUI(t, false)

	previous := askClear
	askClear = func(_ *gui, _ int, answer func(bool)) { answer(true) }
	t.Cleanup(func() { askClear = previous })

	staging := g.stagingDir()
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(staging, "waiting.txt")
	if err := os.WriteFile(staged, []byte("still here"), 0o600); err != nil {
		t.Fatal(err)
	}

	saved := filepath.Join(t.TempDir(), "kept.txt")
	if err := os.WriteFile(saved, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}

	g.mu.Lock()
	g.messages = []chatMessage{
		{text: "one"},
		{from: "laptop", file: &fileNote{name: "waiting.txt", path: staged, state: transferPending}},
		{from: "laptop", file: &fileNote{name: "kept.txt", path: saved, state: transferSaved}},
	}
	g.mu.Unlock()

	g.clearTranscript()

	if len(g.messages) != 0 {
		t.Errorf("Clear all left %d row(s)", len(g.messages))
	}

	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Error("the app's unsaved copy survived Clear all")
	}

	if _, err := os.Stat(saved); err != nil {
		t.Errorf("a saved file was removed by Clear all: %v", err)
	}
}

// Declining the question leaves the conversation exactly as it was.
func TestClearAllDoesNothingWhenDeclined(t *testing.T) {
	g := newScreenGUI(t, false)

	previous := askClear
	askClear = func(_ *gui, _ int, answer func(bool)) { answer(false) }
	t.Cleanup(func() { askClear = previous })

	g.mu.Lock()
	g.messages = []chatMessage{{text: "one"}, {text: "two"}}
	g.mu.Unlock()

	g.clearTranscript()

	if len(g.messages) != 2 {
		t.Errorf("declining Clear all left %d row(s), want 2", len(g.messages))
	}
}

// Copy all is the whole-conversation action, and it is off until there is a
// conversation to copy (#469).
func TestCopyAllCopiesTheWholeTranscript(t *testing.T) {
	g := newScreenGUI(t, false)

	if !g.copyAllBtn.Disabled() {
		t.Error("Copy all is enabled with an empty transcript")
	}

	if !g.selectBtn.Disabled() {
		t.Error("Select is enabled with an empty transcript")
	}

	g.mu.Lock()
	g.messages = []chatMessage{{text: "one"}, {from: "laptop", text: "two"}}
	g.mu.Unlock()
	g.updateButtons()

	if g.copyAllBtn.Disabled() {
		t.Fatal("Copy all is still disabled with two messages")
	}

	g.app.Clipboard().SetContent("")
	g.copyAll()

	want := transcriptOf(g.messages)
	if got := g.app.Clipboard().Content(); got != want {
		t.Errorf("Copy all put %q on the clipboard, want %q", got, want)
	}
}
