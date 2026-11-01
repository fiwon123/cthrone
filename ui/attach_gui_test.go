//go:build gui

package ui

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
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

	// Wait for the send to settle. A row that is still sending offers Cancel in
	// its action slot and a caption with a percentage, which is right — but not
	// what this test is about, and on a slower machine the row below would catch
	// it mid-flight.
	waitForTransferState(t, g, messages[0].file, transferSent)

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
		t.Error("a sent, settled file still offers an action, and the only one there is the receiver's Save")
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

// gatedSession holds the first send until the test lets it go, so a transfer can
// be in flight while the test does something else.
type gatedSession struct {
	*session.Base

	started chan struct{}
	release chan struct{}
}

func (s *gatedSession) Send(payload []byte) error {
	select {
	case s.started <- struct{}{}:
	default:
	}

	<-s.release

	return nil
}

func (s *gatedSession) Close(reason string) { s.NotifyClose(reason) }

// Cancel stops a send in flight, from the row that is showing it, and the row
// goes with it rather than sitting there failed.
func TestCancellingASendStopsIt(t *testing.T) {
	dir := t.TempDir()
	staged := dir + "/.cthrone-send-1"

	// Two chunks, so there is a send after the one that is held.
	body := bytes.Repeat([]byte("x"), int(wire.ChunkSize)+1)
	if err := os.WriteFile(staged, body, 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	gated := &gatedSession{Base: session.NewBase("alice", "alice:8080"), started: make(chan struct{}, 4), release: make(chan struct{})}
	g.mu.Lock()
	g.sess = gated
	g.mu.Unlock()

	note := &fileNote{id: "send-1", name: "clip.mp3", size: int64(len(body)), state: transferSending}
	item := outgoing{id: "send-1", name: "clip.mp3", mime: "audio/mpeg", path: staged, size: int64(len(body)), note: note}

	fyne.Do(func() { g.messages = append(g.messages, chatMessage{file: note}) })

	go g.sendTransfer(item)

	select {
	case <-gated.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the transfer never started sending")
	}

	g.cancelTransfer("send-1")

	close(gated.release)

	waitForStatus(t, g, func() bool { return strings.Contains(g.status.Text(), "cancelled") })

	g.mu.Lock()
	defer g.mu.Unlock()

	for _, message := range g.messages {
		if message.file == note {
			t.Error("a cancelled send left its row behind, want it gone")
		}
	}
}

// Cancel on a receive records the request and the receive loop abandons the
// transfer — the assembler belongs to that goroutine, not to the button.
func TestCancellingAReceiveAbandonsIt(t *testing.T) {
	g := newScreenGUI(t, false)
	g.prepareReceive()

	size := int64(wire.ChunkSize * 2)
	first := wire.Frame{
		Header:  fileChunkHeader("rx-1", "clip.mp3", size, 0),
		Payload: make([]byte, wire.ChunkSize),
	}

	g.receiveTransfer("bob", first)

	g.mu.Lock()
	held := len(g.incoming)
	g.mu.Unlock()

	if held != 1 {
		t.Fatalf("the receive loop holds %d transfers, want 1", held)
	}

	g.cancelTransfer("rx-1")

	// The request is recorded, not acted on: the assembler belongs to the
	// receive loop, so the entry stays — flagged — until that loop sees it.
	g.mu.Lock()
	entry := g.incoming["rx-1"]
	g.mu.Unlock()

	if entry == nil {
		t.Fatal("the cancelled receive vanished before the loop could abandon it")
	}

	if !entry.abandoned {
		t.Error("the cancel was not recorded on the transfer")
	}

	// The next chunk of that transfer is dropped, not written.
	second := wire.Frame{
		Header:  fileChunkHeader("rx-1", "clip.mp3", size, 1),
		Payload: make([]byte, wire.ChunkSize),
	}

	g.receiveTransfer("bob", second)

	g.mu.Lock()
	still := len(g.incoming)
	g.mu.Unlock()

	if still != 0 {
		t.Errorf("the abandoned transfer is still tracked (%d entries)", still)
	}

	if got := g.status.Text(); !strings.Contains(got, "cancelled") {
		t.Errorf("the status line reads %q, want it to say the transfer was cancelled", got)
	}
}

// fileChunkHeader is the first and subsequent chunk header of one transfer, in
// the shape the sender builds.
func fileChunkHeader(id, name string, size int64, index int) wire.Header {
	return wire.Header{
		Kind:   wire.KindFile,
		ID:     id,
		Name:   name,
		MIME:   "audio/mpeg",
		Size:   size,
		Index:  index,
		Chunks: wire.Chunks(size),
	}
}

// recordingSession records what reaches the wire, and holds only file frames:
// text goes straight through while a file is held between chunks. That is the
// property #238 asks for — a transfer in flight does not block the conversation
// — made observable without a socket.
type recordingSession struct {
	*session.Base

	mu      sync.Mutex
	order   []string
	held    chan struct{}
	release chan struct{}
}

func newRecordingSession() *recordingSession {
	return &recordingSession{
		Base:    session.NewBase("alice", "alice:8080"),
		held:    make(chan struct{}, 16),
		release: make(chan struct{}),
	}
}

func (s *recordingSession) Send(payload []byte) error {
	kind := "text"

	if frame, err := wire.Parse(payload); err == nil && frame.Header.Kind == wire.KindFile {
		kind = "file"
	}

	s.mu.Lock()
	s.order = append(s.order, kind)
	s.mu.Unlock()

	if kind == "file" {
		select {
		case s.held <- struct{}{}:
		default:
		}

		<-s.release
	}

	return nil
}

func (s *recordingSession) Close(reason string) { s.NotifyClose(reason) }

func (s *recordingSession) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.order...)
}

// A text message sent while a file is on its way reaches the wire between two
// chunks, not after the file: the conversation does not wait for the transfer.
func TestTextFlowsWhileAFileIsSending(t *testing.T) {
	dir := t.TempDir()
	staged := dir + "/.cthrone-send-1"

	// Two chunks and an end frame, so there is a "between chunks" to land in.
	body := bytes.Repeat([]byte("x"), int(wire.ChunkSize)+1)
	if err := os.WriteFile(staged, body, 0o600); err != nil {
		t.Fatal(err)
	}

	g := newScreenGUI(t, false)

	recorder := newRecordingSession()
	g.mu.Lock()
	g.sess = recorder
	g.mu.Unlock()

	note := &fileNote{id: "send-1", name: "clip.mp3", size: int64(len(body)), state: transferSending}
	item := outgoing{id: "send-1", name: "clip.mp3", mime: "audio/mpeg", path: staged, size: int64(len(body)), note: note}

	go g.sendTransfer(item)

	// The first chunk is held: the file is in flight and the wire is busy.
	select {
	case <-recorder.held:
	case <-time.After(2 * time.Second):
		t.Fatal("the transfer never started sending")
	}

	// A text message while that is true.
	g.send("hello from the composer")

	deadline := time.After(2 * time.Second)

	for {
		kinds := recorder.kinds()
		if len(kinds) >= 2 && kinds[1] == "text" {
			break
		}

		select {
		case <-deadline:
			t.Fatalf("the text never reached the wire while the file was held: %v", kinds)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	// Let the transfer finish.
	go func() {
		for {
			select {
			case <-recorder.held:
			case <-time.After(200 * time.Millisecond):
				return
			}
		}
	}()

	for i := 0; i < 8; i++ {
		select {
		case recorder.release <- struct{}{}:
		default:
		}

		time.Sleep(5 * time.Millisecond)
	}

	kinds := recorder.kinds()

	if len(kinds) < 2 || kinds[0] != "file" {
		t.Fatalf("the wire saw %v, want the file first", kinds)
	}

	if kinds[1] != "text" {
		t.Errorf("the wire saw %v, want the text between the file's chunks", kinds)
	}
}

// waitForTransferState waits for a row's transfer to reach a state, because the
// send and the receive both happen off the UI goroutine: a test that reads the
// row immediately is reading it mid-flight on a machine slower than the one it
// was written on.
func waitForTransferState(t *testing.T, g *gui, note *fileNote, state transferState) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	// The state is written on the UI goroutine, so it is read there too: a test
	// reading it from its own goroutine would be a data race the race detector
	// is right to flag.
	read := func() transferState {
		got := make(chan transferState, 1)
		fyne.Do(func() { got <- note.state })

		return <-got
	}

	for time.Now().Before(deadline) {
		if read() == state {
			return
		}

		time.Sleep(2 * time.Millisecond)
	}

	t.Fatalf("the transfer never reached state %v (it is %v)", state, read())
}
