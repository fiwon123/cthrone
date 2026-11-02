//go:build gui

package ui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// videoNote is a saved animation on disk, the way a received file leaves it.
func videoNote(t *testing.T, name string, body []byte) *fileNote {
	t.Helper()

	path := writeVideo(t, name, body)

	return &fileNote{id: "v", name: name, size: int64(len(body)), state: transferSaved, path: path}
}

// paintVideo paints a row and pulls its player out.
func paintVideo(t *testing.T, g *gui) videoRow {
	t.Helper()

	video, _ := paintVideoRow(t, g)

	return video
}

// paintVideoRow is paintVideo with the row itself, for a test that looks at
// more than the player.
func paintVideoRow(t *testing.T, g *gui) (videoRow, *fyne.Container) {
	t.Helper()

	item := g.chatList.CreateItem()
	g.updateChatRow(0, item)

	row, ok := item.(*fyne.Container)
	if !ok {
		t.Fatalf("the row is %T, want *fyne.Container", item)
	}

	if len(row.Objects) < 7 {
		t.Fatalf("the row has %d objects, want a video player at 6", len(row.Objects))
	}

	video, ok := videoRowOf(row.Objects[6])
	if !ok {
		t.Fatal("the row has no video player")
	}

	return video, row
}

// An animation this build can decode gets a player, and the still preview gives
// the row up to it: two pictures in one row would show frame one forever above
// the one that moves (#421).
func TestAnAnimationGetsAPlayer(t *testing.T) {
	g := newScreenGUI(t, false)
	g.messages = []chatMessage{{from: "laptop", file: videoNote(t, "anim.gif", testGIF(t, 3, 5))}}

	video, row := paintVideoRow(t, g)

	if !video.box.Visible() {
		t.Fatal("an animated GIF hid its player")
	}

	if got := video.play.Text(); got != "Play" {
		t.Errorf("the button says %q, want Play", got)
	}

	if pic, _ := row.Objects[1].(*canvas.Image); pic != nil && pic.Visible() {
		t.Error("the still preview is drawn under the player")
	}
}

// A still picture keeps the inline preview it always had, and gets no player.
func TestAStillPictureHasNoPlayer(t *testing.T) {
	g := newScreenGUI(t, false)
	g.messages = []chatMessage{{from: "laptop", file: &fileNote{
		name: "photo.png", size: 100, state: transferSaved,
		path: writeTestPNG(t), inline: true,
	}}}

	video := paintVideo(t, g)

	if video.box.Visible() {
		t.Error("a PNG was given a video player")
	}
}

// Pressing Play decodes the picture and puts frames on the surface; the button
// becomes Stop while it runs.
func TestPlayingAnAnimationDrawsFrames(t *testing.T) {
	g := newScreenGUI(t, false)
	note := videoNote(t, "anim.gif", testGIF(t, 6, 5)) // 50ms a frame
	g.messages = []chatMessage{{file: note}}

	video := paintVideo(t, g)
	video.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the picture to start", func() bool { return g.videoPlaying(note.path) })

	waitForCondition(t, "a frame on the surface", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()

		return g.videoFrame != nil
	})

	video = paintVideo(t, g)

	if got := video.play.Text(); got != "Stop" {
		t.Errorf("the button says %q while playing, want Stop", got)
	}

	if !video.seek.Visible() {
		t.Error("the seek bar is hidden while an animation plays")
	}
}

// The seek bar moves the picture: the player's position follows the value the
// user chose.
func TestTheSeekBarMovesTheAnimation(t *testing.T) {
	g := newScreenGUI(t, false)
	note := videoNote(t, "anim.gif", testGIF(t, 10, 10)) // 1s
	g.messages = []chatMessage{{file: note}}

	video := paintVideo(t, g)
	video.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the picture to start", func() bool { return g.videoPlaying(note.path) })
	time.Sleep(30 * time.Millisecond)

	video = paintVideo(t, g)
	video.seek.SetValue(0.5)

	position := g.video.Position()
	if position < 400*time.Millisecond || position > 700*time.Millisecond {
		t.Errorf("seeking to 50%% left the player at %v, want about half of 1s", position)
	}
}

// A GIF with one frame is refused with a sentence, and the player leaves the
// row rather than offering a Play that can never work.
func TestAStillGIFLosesThePlayer(t *testing.T) {
	g := newScreenGUI(t, false)
	note := videoNote(t, "still.gif", testGIF(t, 1, 10))
	g.messages = []chatMessage{{file: note}}

	video := paintVideo(t, g)
	video.play.Tapped(&fyne.PointEvent{})

	waitForStatus(t, g, func() bool { return strings.Contains(g.status.Text(), "Cannot play") })

	time.Sleep(20 * time.Millisecond)

	if got := paintVideo(t, g); got.box.Visible() {
		t.Error("the player is still offered for a picture that does not move")
	}
}
