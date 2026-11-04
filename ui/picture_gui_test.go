//go:build gui

package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// pictureRow paints one received picture and returns the row it painted.
func pictureRow(t *testing.T, g *gui, path string) fyne.CanvasObject {
	t.Helper()

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", file: &fileNote{
		id:     "id-1",
		name:   filepath.Base(path),
		size:   testFileSize(t, path),
		state:  transferSaved,
		path:   path,
		inline: true,
	}}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	return row
}

// The first paint of a photo says so instead of waiting for the decode: a
// full-size decode on the UI goroutine is what stalled the scroll (#510).
func TestAPictureRowSaysItIsLoadingBeforeTheThumbnailLands(t *testing.T) {
	g := newScreenGUI(t, false)

	block := make(chan struct{})
	previous := decodeThumb
	decodeThumb = func(string) (fyne.Resource, error) {
		<-block

		return stubPicture(), nil
	}
	t.Cleanup(func() { decodeThumb = previous })

	row := pictureRow(t, g, writeTestPNG(t))

	_, pic, preview, _, _ := chatRowObjects(t, row)

	if pic.Visible() {
		t.Error("a picture was drawn before its thumbnail was decoded")
	}

	if !preview.Visible() || !strings.Contains(preview.Text(), "loading") {
		t.Errorf("the row says %q, want that the picture is loading", preview.Text())
	}
}

// Once the thumbnail is in the cache the row draws it, and the note goes away.
func TestAThumbnailInTheCacheIsDrawnAtOnce(t *testing.T) {
	g := newScreenGUI(t, false)

	path := writeTestPNG(t)

	g.putPicture(path, stubPicture())

	row := pictureRow(t, g, path)

	_, pic, preview, _, _ := chatRowObjects(t, row)

	if !pic.Visible() {
		t.Error("a cached thumbnail was not drawn")
	}

	if preview.Visible() {
		t.Error("the row still says a picture is loading, though it was drawn")
	}
}

// The cache is bounded and drops the oldest, not everything: scrolling back
// through a conversation of photos must not decode them all again (#510).
func TestTheThumbnailCacheDropsTheOldest(t *testing.T) {
	g := newScreenGUI(t, false)

	for i := 0; i <= previewCacheMax; i++ {
		g.cacheMu.Lock()
		g.putPicture(pathN(i), stubPicture())
		g.cacheMu.Unlock()
	}

	g.cacheMu.Lock()
	_, oldest := g.pictures[pathN(0)]
	_, newest := g.pictures[pathN(previewCacheMax)]
	size := len(g.pictures)
	g.cacheMu.Unlock()

	if oldest {
		t.Error("the oldest thumbnail is still cached")
	}

	if !newest {
		t.Error("the newest thumbnail was dropped")
	}

	if size != previewCacheMax {
		t.Errorf("the cache holds %d thumbnails, want %d", size, previewCacheMax)
	}
}

// pathN is a cache key for the eviction test.
func pathN(n int) string {
	return "/pictures/" + strings.Repeat("x", n+1) + ".png"
}

// The copy action puts that row's picture on the system clipboard, through the
// seam a test can watch (#510).
func TestAPictureCanBeCopiedToTheSystemClipboard(t *testing.T) {
	g := newScreenGUI(t, false)

	path := writeTestPNG(t)

	copied := ""
	previous := copyImageToOS
	copyImageToOS = func(p string) error {
		copied = p

		return nil
	}
	t.Cleanup(func() { copyImageToOS = previous })

	row := pictureRow(t, g, path)

	var button *widgets.CopyButton

	for _, object := range row.(*fyne.Container).Objects {
		if candidate, ok := object.(*widgets.CopyButton); ok {
			button = candidate
		}
	}

	if button == nil {
		t.Fatal("the row carries no actions at all")
	}

	g.copyPictureNow(path)

	if copied != path {
		t.Errorf("the clipboard got %q, want %q", copied, path)
	}

	if got := g.status.Text(); !strings.Contains(got, "copied") {
		t.Errorf("the status says %q, want that the picture was copied", got)
	}
}

// A platform without an image clipboard says so, and names the way out.
func TestCopySaysWhenThereIsNoImageClipboard(t *testing.T) {
	g := newScreenGUI(t, false)

	previous := copyImageToOS
	copyImageToOS = func(string) error { return errNoImageClipboard }
	t.Cleanup(func() { copyImageToOS = previous })

	g.copyPictureNow(writeTestPNG(t))

	if got := g.status.Text(); !strings.Contains(got, "Save as") {
		t.Errorf("the status says %q, want it to name Save as…", got)
	}
}

// The copy action only exists where there is a picture to copy.
func TestATextRowOffersNoPictureCopy(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "hello"}}
	g.mu.Unlock()

	row := g.chatList.CreateItem()
	g.updateChatRow(0, row)

	for i, object := range row.(*fyne.Container).Objects {
		if button, ok := object.(*widgets.CopyButton); ok && button.Visible() && i == len(row.(*fyne.Container).Objects)-1 {
			t.Error("a text row offers the picture copy action")
		}
	}
}

var _ = errors.Is
