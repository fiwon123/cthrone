//go:build gui

package ui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/storage"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// fakePick is what a phone's picker hands over: a content:// URI with a stream,
// and no path on this disk.
type fakePick struct {
	uri  fyne.URI
	data []byte
	err  error
}

func (p *fakePick) URI() fyne.URI { return p.uri }

func (p *fakePick) Read(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}

	if len(p.data) == 0 {
		return 0, io.EOF
	}

	n := copy(b, p.data)
	p.data = p.data[n:]

	return n, nil
}

func (p *fakePick) Close() error { return nil }

// contentPick is a pick the way Android makes one: a content:// URI, so there is
// no path the row could read after the staged copy is gone.
func contentPick(t *testing.T, data []byte) *fakePick {
	t.Helper()

	uri, err := storage.ParseURI("content://com.android.providers.downloads.documents/document/42")
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}

	return &fakePick{uri: uri, data: data}
}

// jpegBytes is a small picture, encoded the way a camera's file would be.
func jpegBytes(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	return buf.Bytes()
}

// waitForRows waits for the pick's goroutine to add its row.
func waitForRows(t *testing.T, g *gui, want int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		got := len(g.messages)
		g.mu.Unlock()

		if got >= want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("the transcript holds fewer than %d rows after the pick", want)
}

// A picture picked on a phone has no path the row can read, so the row keeps a
// thumbnail instead — and that is what the user sees, where a caption alone was
// the report (#303).
func TestAPicturePickedWithNoPathShowsAThumbnail(t *testing.T) {
	g := newScreenGUI(t, true)

	g.queuePick(contentPick(t, jpegBytes(t)))
	waitForRows(t, g, 1)

	g.mu.Lock()
	note := g.messages[0].file
	g.mu.Unlock()

	if note == nil {
		t.Fatal("the pick added no file row")
	}

	if note.path != "" {
		t.Errorf("the row has path %q, but a content:// pick has none", note.path)
	}

	if len(note.thumb) == 0 {
		t.Fatal("the row kept no thumbnail, so it has no picture to draw")
	}

	// The thumbnail is a PNG the row can draw, at the height the transcript
	// draws pictures.
	img, err := png.Decode(bytes.NewReader(note.thumb))
	if err != nil {
		t.Fatalf("the thumbnail is not a decodable PNG: %v", err)
	}

	if got := img.Bounds().Dy(); got != inlineImageHeight {
		t.Errorf("the thumbnail is %d px tall, want %d", got, inlineImageHeight)
	}

	if got := img.Bounds().Dx(); got != 320*inlineImageHeight/240 {
		t.Errorf("the thumbnail is %d px wide, want the picture's aspect at that height", got)
	}
}

// The row draws the thumbnail when there is no path — the caption alone was the
// report.
func TestAFileRowDrawsItsThumbnailWhenItHasNoPath(t *testing.T) {
	g := newScreenGUI(t, true)

	pic := canvas.NewImageFromResource(nil)
	preview := g.text("")
	save := widgets.NewButton("Save as…", nil, nil)
	open := widgets.NewButtonWithIcon("Open", nil, nil, nil)

	picture := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(picture, jpegBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}

	thumb := thumbnailPNG(picture, inlineImageHeight)
	if len(thumb) == 0 {
		t.Fatal("the test picture produced no thumbnail")
	}

	note := &fileNote{id: "id-1", name: "photo.jpg", size: 1000, state: transferSent, thumb: thumb, inline: true}

	g.fillFileRow(chatMessage{from: "me", file: note}, pic, preview, save, open)

	if !pic.Visible() {
		t.Error("the row hid the picture although it holds a thumbnail")
	}

	if res := pic.Resource; res == nil || len(res.Content()) == 0 {
		t.Error("the picture was shown without the thumbnail as its content")
	}

	if open.Visible() || save.Visible() {
		t.Error("a row with no path offered an action that needs one")
	}
}

// A send is staged in the app's own folder, not among the user's downloads
// (#303).
func TestASendIsStagedInTheAppsFolder(t *testing.T) {
	g := newScreenGUI(t, false)

	path, size, err := g.stageForSend(bytes.NewReader([]byte("the bytes")))
	if err != nil {
		t.Fatalf("stageForSend: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if got, want := filepath.Dir(path), g.stagingDir(); got != want {
		t.Errorf("staged in %q, want the app's staging folder %q", got, want)
	}

	if size != int64(len("the bytes")) {
		t.Errorf("staged %d bytes, want %d", size, len("the bytes"))
	}
}

// A pick that fails leaves a row, so "my picture did not send" has something to
// look at afterwards (#303).
func TestAPickThatFailsLeavesARow(t *testing.T) {
	g := newScreenGUI(t, true)

	pick := contentPick(t, nil)
	pick.err = errors.New("the provider went away")

	g.queuePick(pick)
	waitForRows(t, g, 1)

	g.mu.Lock()
	note := g.messages[0].file
	g.mu.Unlock()

	if note == nil {
		t.Fatal("the failed pick left no row")
	}

	if note.state != transferFailed {
		t.Errorf("the row is in state %v, want failed", note.state)
	}

	if note.name != "42" && note.name == "" {
		t.Errorf("the row does not name the file that failed: %q", note.name)
	}
}

// Sending must not need the download folder at all: a pick is staged in the
// app's own folder, so a download folder that cannot be created does not stop a
// send (#303).
func TestASendDoesNotNeedTheDownloadFolder(t *testing.T) {
	g := newScreenGUI(t, false)

	// The default download folder is $HOME/Downloads; a file of that name makes
	// it impossible to create, which is what a broken folder looks like.
	home := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(home, "Downloads"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	g.queuePick(contentPick(t, jpegBytes(t)))
	waitForRows(t, g, 1)

	g.mu.Lock()
	note := g.messages[0].file
	g.mu.Unlock()

	if note == nil {
		t.Fatal("the send added no row")
	}

	if note.state == transferFailed {
		t.Error("the send failed because of the download folder, which staging must not use")
	}
}
