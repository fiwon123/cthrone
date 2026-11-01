//go:build gui

package ui

import (
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"
)

// pickedFile is what the file picker hands over: a reader that knows its name.
type pickedFile struct {
	*os.File
	uri fyne.URI
}

func (f pickedFile) URI() fyne.URI { return f.uri }

// The sender sees what the receiver sees. Before #237 a sent picture produced a
// row that only said "sent", while the same bytes drawn on the other device
// showed the picture — two devices looking at one conversation, seeing
// different conversations.
func TestSentPictureDrawsTheSamePreviewAsAReceivedOne(t *testing.T) {
	path := writeTestPNG(t)

	g := newScreenGUI(t, false)

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	// A session that accepts the bytes, so the row ends up "sent" rather than
	// "failed", and the staged copy is removed the way a real send removes it.
	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.queuePick(pickedFile{File: file, uri: storage.NewFileURI(path)})

	messages := waitForMessages(t, g, 1)
	if messages[0].file == nil {
		t.Fatalf("the first row is not a transfer: %+v", messages[0])
	}

	// The row has to carry a path this device can still read, or there is
	// nothing to preview from — the staged copy is deleted once the send
	// returns.
	if got := messages[0].file.path; got != path {
		t.Fatalf("the sent file points at %q, want the user's own file %q", got, path)
	}

	if !messages[0].file.inline {
		t.Errorf("a sent %q is not marked for an inline preview, and the same file received would be", path)
	}

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	label, pic, _, save, _ := chatRowObjects(t, row)

	if !pic.Visible() {
		t.Error("a sent picture drew no picture in its own transcript")
	}

	if pic.Resource == nil || pic.Aspect() <= 0 {
		t.Error("the picture is visible but decodes to nothing")
	}

	// The caption is still the sender's: it says the file was sent, not saved,
	// so the row is not pretending to be the receiver's.
	if !strings.Contains(label.Text(), "sent") {
		t.Errorf("the caption = %q, want it to say the file was sent", label.Text())
	}

	if save.Visible() {
		t.Error("a sent file offered Save as…, which is the receiver's action")
	}
}

// A file that is not a picture stays a file row on both sides: the preview is
// for what can be shown, not for everything that moves.
func TestSentTextFileStaysAFileRow(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notes.txt"

	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.queuePick(pickedFile{File: file, uri: storage.NewFileURI(path)})

	messages := waitForMessages(t, g, 1)
	if messages[0].file == nil {
		t.Fatalf("the first row is not a transfer: %+v", messages[0])
	}

	if messages[0].file.inline {
		t.Error("a text file was marked inline, and a received one would not be")
	}

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, _, _, _ := chatRowObjects(t, row)

	if pic.Visible() {
		t.Error("a sent text file drew a picture")
	}
}

// The row's picture is not decoration: tapping it opens the file, which is what
// tapping a received picture does.
func TestTappingASentPictureOpensIt(t *testing.T) {
	path := writeTestPNG(t)

	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{file: &fileNote{
		name:   "pic.png",
		size:   testFileSize(t, path),
		state:  transferSent,
		path:   path,
		inline: true,
	}}}
	g.mu.Unlock()

	g.selectChatRow(0)

	if g.win.Canvas().Overlays().Top() == nil {
		t.Error("tapping a sent picture opened nothing, where the same received picture opens a viewer")
	}
}

// A file that claims to be a picture but cannot be decoded stays a file row —
// on the sending side too. A blank box in the transcript reads as a failed
// transfer, which is the bug #237 asks not to have.
func TestUndecodablePictureStaysAFileRow(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/huge.png"

	if err := os.WriteFile(path, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	g.queuePick(pickedFile{File: file, uri: storage.NewFileURI(path)})

	messages := waitForMessages(t, g, 1)
	if messages[0].file == nil {
		t.Fatalf("the first row is not a transfer: %+v", messages[0])
	}

	// A .png name over a body that is not one: the decision follows the type and
	// the size, and the row hides the picture when the decode fails — which is
	// the "unknown or corrupt file still renders as a file row" rule.
	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, _, _, _ := chatRowObjects(t, row)
	if pic.Visible() {
		t.Error("a file that does not decode drew a picture")
	}
}

// A text file is drawn in the transcript, on both sides: the sender's row and
// the receiver's row go through one function, so a log the user sent reads the
// same as the one the other person received (#237).
func TestTextFileIsDrawnInTheRow(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notes.txt"

	if err := os.WriteFile(path, []byte("the first line\nthe second line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{file: &fileNote{
		name:  "notes.txt",
		size:  31,
		state: transferSaved,
		path:  path,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, preview, save, _ := chatRowObjects(t, row)

	if pic.Visible() {
		t.Error("a text file drew a picture")
	}

	if !preview.Visible() {
		t.Fatal("a text file drew no text")
	}

	for _, want := range []string{"the first line", "the second line"} {
		if !strings.Contains(preview.Text(), want) {
			t.Errorf("the preview = %q, want it to contain %q", preview.Text(), want)
		}
	}

	// It is selectable text like everything else a user reads: a desktop drags
	// over it, a phone long-presses it.
	if !preview.Selectable() {
		t.Error("a desktop cannot select the text it is reading")
	}

	if !save.Visible() {
		t.Error("a saved text file does not offer Save as…")
	}
}

// A binary is not drawn as text: the row stays a filename and an action.
func TestBinaryFileDrawsNoText(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/blob.bin"

	if err := os.WriteFile(path, []byte{0x00, 0xff, 0x01}, 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{file: &fileNote{
		name:  "blob.bin",
		size:  3,
		state: transferSaved,
		path:  path,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, _, preview, _, _ := chatRowObjects(t, row)

	if preview.Visible() {
		t.Error("a binary file was drawn as text")
	}
}

// A file this program cannot draw is offered to the device: a PDF or an audio
// clip opens in whatever handles that kind of file here. The button only appears
// where it can do something — the file is on this disk and the row is not
// already showing its content.
func TestUndrawableFileOffersOpen(t *testing.T) {
	dir := t.TempDir()
	pdf := dir + "/manual.pdf"

	if err := os.WriteFile(pdf, []byte("%PDF-1.4 not really"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)
	recorder := withOpener(t)

	g.mu.Lock()
	g.messages = []chatMessage{{file: &fileNote{
		name:  "manual.pdf",
		size:  18,
		state: transferSaved,
		path:  pdf,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, preview, save, open := chatRowObjects(t, row)

	if pic.Visible() || preview.Visible() {
		t.Error("a PDF was drawn as a picture or as text, and this program has no engine for either")
	}

	if !save.Visible() {
		t.Error("a saved PDF does not offer Save as…")
	}

	if !open.Visible() {
		t.Fatal("a saved PDF offers no way to open it")
	}

	test.Tap(open)

	if len(recorder.opened) != 1 {
		t.Fatalf("Open asked for %d addresses, want one", len(recorder.opened))
	}

	if got := recorder.opened[0]; !strings.HasPrefix(got, "file://") || !strings.HasSuffix(got, "manual.pdf") {
		t.Errorf("Open asked for %q, want the file's own URI", got)
	}
}

// A row that already draws its content offers no Open: the picture is there, and
// a button that opens it again in another window is noise.
func TestDrawnFileOffersNoOpen(t *testing.T) {
	path := writeTestPNG(t)

	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{file: &fileNote{
		name:   "pic.png",
		size:   testFileSize(t, path),
		state:  transferSaved,
		path:   path,
		inline: true,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, _, _, open := chatRowObjects(t, row)

	if !pic.Visible() {
		t.Fatal("the picture was not drawn")
	}

	if open.Visible() {
		t.Error("a drawn picture also offered Open")
	}
}
