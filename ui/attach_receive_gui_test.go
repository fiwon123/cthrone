//go:build gui

package ui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/wire"
)

// A whole file goes through the receive path: the frames the sender produces,
// parsed and fed the way the session feeds them, land as one file with the
// bytes that were sent.
//
// No test did this before (#286): the receive tests stopped at one chunk, at a
// cancel and at an abort, so nothing proved that a finished transfer reaches the
// app's storage. The default is manual now (#469), so that is where it stays
// until the row's Save as… is pressed.
func TestAWholeFileArrivesThroughTheReceivePath(t *testing.T) {
	g := newScreenGUI(t, false)
	g.prepareReceive()

	body := bytes.Repeat([]byte("cthrone "), 40_000) // 320 KB: two chunks
	recorder := newSendRecorder()

	if err := sendFile(context.Background(), recorder, "id-1", "notes.txt", "text/plain", int64(len(body)), bytes.NewReader(body), nil); err != nil {
		t.Fatalf("sendFile: %v", err)
	}

	recorder.mu.Lock()
	frames := append([][]byte(nil), recorder.sent...)
	recorder.mu.Unlock()

	for _, frame := range frames {
		g.receiveFrame("laptop", frame)
	}

	if len(g.messages) != 1 || g.messages[0].file == nil {
		t.Fatalf("the transcript holds %d rows, want one file row", len(g.messages))
	}

	note := g.messages[0].file
	if state := note.state; state != transferPending {
		t.Errorf("the row is in state %v, want not saved", state)
	}

	got, err := os.ReadFile(note.path)
	if err != nil {
		t.Fatalf("the file did not stay in the app's storage: %v", err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("the file that landed has %d bytes, want the %d that were sent", len(got), len(body))
	}
}

// Automatic saving is the opt-in: the same arrival is moved into the chosen
// folder at once, and the row says where it went (#469).
func TestAutoSavePutsTheFileInTheFolder(t *testing.T) {
	g := newScreenGUI(t, false)
	folder := g.fileSettings().folder
	g.storeFileSettings(folder, true)
	g.prepareReceive()

	body := bytes.Repeat([]byte("cthrone "), 4_000)
	recorder := newSendRecorder()

	if err := sendFile(context.Background(), recorder, "id-1", "notes.txt", "text/plain", int64(len(body)), bytes.NewReader(body), nil); err != nil {
		t.Fatalf("sendFile: %v", err)
	}

	recorder.mu.Lock()
	frames := append([][]byte(nil), recorder.sent...)
	recorder.mu.Unlock()

	for _, frame := range frames {
		g.receiveFrame("laptop", frame)
	}

	saved := filepath.Join(folder, "notes.txt")

	got, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the file did not land in the download folder: %v", err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("the file that landed has %d bytes, want the %d that were sent", len(got), len(body))
	}

	if state := g.messages[0].file.state; state != transferSaved {
		t.Errorf("the row is in state %v, want saved", state)
	}
}

// A file that cannot be put in the folder arrived all the same: the row says it
// is waiting to be saved and keeps the bytes reachable. It used to say "failed"
// and blank the path, which told the user a file that had arrived had not
// (#286).
func TestAFileThatCannotBeSavedIsNotAFailedTransfer(t *testing.T) {
	g := newScreenGUI(t, false)

	staged := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(staged, []byte("the whole file"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A folder that cannot be created: its parent is a regular file.
	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	note := &fileNote{id: "id-1", name: "report.pdf", size: 100, state: transferSending}
	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", file: note}}
	g.mu.Unlock()

	g.saveTo(note, &wire.File{Name: "report.pdf", Path: staged, Size: 100, MIME: "application/pdf"}, filepath.Join(blocked, "downloads"))

	if note.state != transferPending {
		t.Errorf("a save failure left the row in state %v, want waiting to be saved", note.state)
	}

	if note.path != staged {
		t.Errorf("the row points at %q, want the staged file %q", note.path, staged)
	}

	if _, err := os.Stat(staged); err != nil {
		t.Errorf("the bytes that arrived were lost: %v", err)
	}

	if !strings.Contains(g.status.Text(), "could not save") {
		t.Errorf("the status line does not say the save failed: %q", g.status.Text())
	}
}

// A folder that cannot be written is refused when it is chosen, and the folder
// already in force stays. Remembering it used to fail every later file in
// silence, which is what a phone's folder picker produced (#286).
func TestAFolderThatCannotBeWrittenIsNotRemembered(t *testing.T) {
	g := newScreenGUI(t, false)

	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	g.storeFileSettings(filepath.Join(blocked, "downloads"), false)

	if got := g.preferences().String(prefsDownloadFolder); got != "" {
		t.Errorf("the unusable folder was remembered as %q", got)
	}

	if !strings.Contains(g.status.Text(), "Not saved") {
		t.Errorf("the status line does not say the folder was refused: %q", g.status.Text())
	}
}

// A device joining the room is not a new connection for the one already in it:
// the assembler a running transfer writes into must survive the join (#286).
func TestASecondDeviceJoiningKeepsTheAssembler(t *testing.T) {
	g := newScreenGUI(t, false)

	first := newSendRecorder()
	second := newSendRecorder()

	g.attach(first)

	g.mu.Lock()
	assembler := g.assembler
	g.mu.Unlock()

	g.attach(second)

	g.mu.Lock()
	same := g.assembler
	current := g.sess
	g.mu.Unlock()

	if assembler == nil {
		t.Fatal("the first session installed no assembler")
	}

	if same != assembler {
		t.Error("the second device's join replaced the assembler, so a transfer in flight would fail")
	}

	if current != second {
		t.Error("the newest session is not the one the window sends through")
	}
}

// A device leaving the room must not take the receiver with it: the other device
// is still connected and still sends files (#342).
func TestAFileArrivesAfterAnotherDeviceLeaves(t *testing.T) {
	g := newScreenGUI(t, false)
	g.storeFileSettings(g.fileSettings().folder, true)

	first := recorder(t, "laptop")
	second := recorder(t, "phone")

	g.attach(first)
	g.attach(second)

	g.mu.Lock()
	before := g.assembler
	g.mu.Unlock()

	// The newest session leaves, which is the one g.sess pointed at.
	second.Close("the phone went away")

	g.mu.Lock()
	after := g.assembler
	live := g.liveSessions
	g.mu.Unlock()

	if before == nil {
		t.Fatal("the first session installed no assembler")
	}

	if after != before {
		t.Error("a device leaving the room closed the receiver, so the remaining device cannot send a file")
	}

	if live != 1 {
		t.Errorf("the room counts %d live session(s), want 1", live)
	}

	// And a file from the device that stayed is accepted rather than refused.
	body := bytes.Repeat([]byte("still here "), 4_000)
	recorderOut := newSendRecorder()

	if err := sendFile(context.Background(), recorderOut, "id-1", "notes.txt", "text/plain", int64(len(body)), bytes.NewReader(body), nil); err != nil {
		t.Fatalf("sendFile: %v", err)
	}

	recorderOut.mu.Lock()
	frames := append([][]byte(nil), recorderOut.sent...)
	recorderOut.mu.Unlock()

	for _, frame := range frames {
		g.receiveFrame("laptop", frame)
	}

	saved := filepath.Join(g.fileSettings().folder, "notes.txt")
	got, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the remaining device's file did not land: %v", err)
	}

	if !bytes.Equal(got, body) {
		t.Errorf("the file that landed has %d bytes, want %d", len(got), len(body))
	}
}

// A client's session ending is the end of its receiving, as before.
func TestAClientsCloseEndsItsReceiving(t *testing.T) {
	g := newScreenGUI(t, false)

	only := recorder(t, "host")
	g.attach(only)

	g.mu.Lock()
	before := g.assembler
	g.mu.Unlock()

	only.Close("the host stopped")

	g.mu.Lock()
	after := g.assembler
	g.mu.Unlock()

	if before == nil {
		t.Fatal("the session installed no assembler")
	}

	if after != nil {
		t.Error("the receiver survived its only session, so a half file is kept for a connection that is gone")
	}
}
