//go:build gui

package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// These tests drive the real widget tree with Fyne's headless test driver.
//
// The `ci` build tag is what makes them runnable here: it swaps Fyne's GL
// driver for the software one (fyne.io/fyne/v2/app/app_software.go), so the
// gui-tagged package compiles and its tests run without GTK3/X11/GL:
//
//	CGO_ENABLED=0 go test -tags "gui ci" ./ui/...
//
// They assert the two behaviours #108 proved for the transport but never for
// the screen: a progress row per transfer, and a decodable image drawn inline.

// newTestGUI builds the window the way run() does, with the test driver and a
// configuration directory of its own.
func newTestGUI(t *testing.T) *gui {
	t.Helper()

	// The device key and the pins live under os.UserConfigDir; a test must not
	// read or write the real ones. XDG_CONFIG_HOME covers Linux, HOME covers
	// macOS, which ignores XDG.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	app := test.NewApp()
	t.Cleanup(app.Quit)

	win := app.NewWindow("cthrone")
	g := &gui{app: app, win: win}
	g.build()
	t.Cleanup(g.stopCopyHint)

	return g
}

// chatRowObjects pulls a transcript row apart into its parts: the caption, the
// inline picture, the text preview, and the Save action.
func chatRowObjects(t *testing.T, row fyne.CanvasObject) (*widgets.Text, *canvas.Image, *widgets.Text, *widgets.CopyButton, *widgets.CopyButton) {
	t.Helper()

	box, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("row is %T, want *fyne.Container", row)
	}

	if len(box.Objects) != 5 {
		t.Fatalf("row has %d objects, want 5", len(box.Objects))
	}

	label, ok := box.Objects[0].(*widgets.Text)
	if !ok {
		t.Fatalf("row[0] is %T, want *widgets.Text", box.Objects[0])
	}

	pic, ok := box.Objects[1].(*canvas.Image)
	if !ok {
		t.Fatalf("row[1] is %T, want *canvas.Image", box.Objects[1])
	}

	preview, ok := box.Objects[2].(*widgets.Text)
	if !ok {
		t.Fatalf("row[2] is %T, want the text preview", box.Objects[2])
	}

	save, ok := box.Objects[3].(*widgets.CopyButton)
	if !ok {
		t.Fatalf("row[3] is %T, want *widgets.CopyButton", box.Objects[3])
	}

	open, ok := box.Objects[4].(*widgets.CopyButton)
	if !ok {
		t.Fatalf("row[4] is %T, want the Open action", box.Objects[4])
	}

	return label, pic, preview, save, open
}

// TestChatRowReportsTransferProgress checks that each transfer draws its own
// progress line. Two rows with different counts must show different
// percentages: a single shared row would show the same number twice.
func TestChatRowReportsTransferProgress(t *testing.T) {
	g := newTestGUI(t)

	g.messages = []chatMessage{
		{file: &fileNote{name: "a.bin", size: 1000, sent: 250, state: transferSending}},
		{file: &fileNote{name: "b.bin", size: 2048, sent: 2048, state: transferSending}},
	}

	tests := []struct {
		name string
		id   int
		want string
	}{
		{"a quarter through", 0, "You: a.bin — 25% of 1000 B"},
		{"complete", 1, "You: b.bin — 100% of 2.0 KB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := g.chatList.CreateItem()
			g.updateChatRow(tt.id, row)

			label, pic, _, save, _ := chatRowObjects(t, row)

			if label.Text() != tt.want {
				t.Errorf("row %d = %q, want %q", tt.id, label.Text(), tt.want)
			}

			if pic.Visible() {
				t.Error("a transfer row drew a picture")
			}

			if save.Visible() {
				t.Error("an outgoing transfer offered Save")
			}
		})
	}
}

// TestChatRowDrawsADecodableImageInline checks that a picture the toolkit can
// decode is drawn in the transcript: the row shows the image, not an empty
// box. Aspect is what proves the decode happened — it stays zero when the
// bytes are not an image.
func TestChatRowDrawsADecodableImageInline(t *testing.T) {
	path := writeTestPNG(t)

	g := newTestGUI(t)
	g.messages = []chatMessage{{file: &fileNote{
		name:   "pic.png",
		size:   testFileSize(t, path),
		state:  transferSaved,
		path:   path,
		inline: true,
	}}}

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	label, pic, _, save, _ := chatRowObjects(t, row)

	if !pic.Visible() {
		t.Fatal("a decodable image was not drawn inline")
	}

	if pic.Resource == nil {
		t.Fatal("the picture is visible but has no resource")
	}

	if got := pic.Aspect(); got <= 0 {
		t.Errorf("picture aspect = %v, want > 0: the toolkit did not decode it", got)
	}

	if got := pic.MinSize().Height; got != inlineImageHeight {
		t.Errorf("picture height = %v, want %d", got, inlineImageHeight)
	}

	if !save.Visible() {
		t.Error("a saved file did not offer Save")
	}

	if !strings.Contains(label.Text(), "saved") {
		t.Errorf("caption = %q, want it to say the file was saved", label.Text())
	}
}

// TestChatRowKeepsAPictureHiddenWhenItIsNotInline is the other half of the
// rule: a valid image that was not marked inline stays a file row, so the
// transcript never grows a picture the sender did not ask to show.
func TestChatRowKeepsAPictureHiddenWhenItIsNotInline(t *testing.T) {
	path := writeTestPNG(t)

	g := newTestGUI(t)
	g.messages = []chatMessage{{file: &fileNote{
		name:  "pic.png",
		size:  testFileSize(t, path),
		state: transferSaved,
		path:  path,
	}}}

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	_, pic, _, save, _ := chatRowObjects(t, row)

	if pic.Visible() {
		t.Error("a file row that is not inline drew a picture")
	}

	if !save.Visible() {
		t.Error("a saved file did not offer Save")
	}
}

// writeTestPNG writes a small, valid PNG and returns its path.
func writeTestPNG(t *testing.T) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		for y := 0; y < 2; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 120), B: 200, A: 255})
		}
	}

	path := filepath.Join(t.TempDir(), "pic.png")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the test image: %v", err)
	}

	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatalf("encoding the test image: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("closing the test image: %v", err)
	}

	return path
}

// testFileSize is the size a fileNote would carry for the file at path.
func testFileSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("statting %s: %v", path, err)
	}

	return info.Size()
}

// guiStubSession is a session the widget tree can attach to without a socket.
type guiStubSession struct {
	*session.Base
}

func (s *guiStubSession) Send([]byte) error   { return nil }
func (s *guiStubSession) Close(reason string) { s.NotifyClose(reason) }

// TestAttachReportsAPeerDisconnect checks that a peer leaving leaves a line in
// the transcript: the status line changes, but a host reading the chat after
// the room is empty needs the conversation to say who left.
func TestAttachReportsAPeerDisconnect(t *testing.T) {
	g := newTestGUI(t)
	s := &guiStubSession{Base: session.NewBase("id", "192.168.1.5:5555")}

	g.attach(s)
	s.NotifyMessage(session.Message{From: "laptop", Payload: []byte("hello")})
	s.Close("peer said goodbye")

	last := g.messages[len(g.messages)-1]
	if last.from != "system" {
		t.Fatalf("last message is from %q, want the system", last.from)
	}

	if !strings.Contains(last.text, "laptop disconnected") {
		t.Errorf("last message = %q, want the disconnect notice", last.text)
	}

	if !strings.Contains(last.text, "peer said goodbye") {
		t.Errorf("last message = %q, want the reason", last.text)
	}
}

// TestStopLeavesNoDisconnectLine checks the other side of it: a session the
// user ended is not a peer that left, so Stop must not report one.
func TestStopLeavesNoDisconnectLine(t *testing.T) {
	g := newTestGUI(t)
	s := &guiStubSession{Base: session.NewBase("id", "192.168.1.5:5555")}

	g.attach(s)

	before := len(g.messages)

	// stop() clears the live session before it closes it; the callback must see
	// that this session was no longer the live one and say nothing.
	g.mu.Lock()
	g.sess = nil
	g.mu.Unlock()

	s.Close("closed from the app")

	if after := len(g.messages); after != before {
		t.Errorf("transcript grew from %d to %d messages after a user stop", before, after)
	}
}
