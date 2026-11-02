//go:build gui

package ui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testGIF builds an animation with frames frames, each lasting delay
// hundredths of a second. Every frame is a flat colour, so a test can tell them
// apart by their pixels.
func testGIF(t *testing.T, frames, delay int) []byte {
	t.Helper()

	palette := color.Palette{color.Black, color.White}

	animation := &gif.GIF{
		Config: image.Config{ColorModel: palette, Width: 4, Height: 4},
	}

	for i := 0; i < frames; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)

		for j := range frame.Pix {
			frame.Pix[j] = uint8(i % 2)
		}

		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, delay)
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, animation); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// writeVideo puts bytes in a temporary file.
func writeVideo(t *testing.T, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// A GIF decodes into one frame per image, each with its own delay, and the
// delay is hundredths of a second (#421).
func TestDecodeGIFReadsEveryFrame(t *testing.T) {
	clip, err := decodeVideo(writeVideo(t, "anim.gif", testGIF(t, 3, 10)))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	if len(clip.frames) != 3 {
		t.Fatalf("%d frames, want 3", len(clip.frames))
	}

	if got, want := clip.duration, 300*time.Millisecond; got != want {
		t.Errorf("duration = %v, want %v", got, want)
	}

	for i, delay := range clip.delays {
		if delay != 100*time.Millisecond {
			t.Errorf("frame %d lasts %v, want 100ms", i, delay)
		}
	}
}

// A GIF with one frame is a picture, not a video: drawing it as one would take
// the row away from the path that already draws pictures.
func TestDecodeGIFRefusesAStillPicture(t *testing.T) {
	path := writeVideo(t, "still.gif", testGIF(t, 1, 10))

	if _, err := decodeVideo(path); !errors.Is(err, errVideoUnsupported) {
		t.Errorf("decodeVideo() error = %v, want %v", err, errVideoUnsupported)
	}
}

// A format with no decoder here is refused; so is a file past the size bound.
func TestDecodeVideoRefusesWhatItCannotPlay(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}

	if _, err := decodeVideo(writeVideo(t, "still.png", picture.Bytes())); !errors.Is(err, errVideoUnsupported) {
		t.Errorf("a PNG decoded as video: %v", err)
	}

	// A real GIF header with a body far past the bound.
	huge := append(testGIF(t, 2, 10), make([]byte, videoMaxBytes+1)...)

	if _, err := decodeVideo(writeVideo(t, "huge.gif", huge)); !errors.Is(err, errVideoTooBig) {
		t.Errorf("an oversized file decoded: %v", err)
	}
}

// The decoder is chosen from the bytes, not the name.
func TestVideoKindReadsTheHeader(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		want string
	}{
		{"gif87", []byte("GIF87a...."), "gif"},
		{"gif89", []byte("GIF89a...."), "gif"},
		{"png", []byte("\x89PNG\r\n\x1a\n"), ""},
		{"mp4", []byte("\x00\x00\x00\x20ftypisom"), ""},
		{"webm", []byte("\x1aE\xdf\xa3...."), ""},
		{"short", []byte("GIF"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := videoKind(tt.head); got != tt.want {
				t.Errorf("videoKind(%q) = %q, want %q", tt.head, got, tt.want)
			}
		})
	}
}

// A GIF frame may cover part of the picture; what it does not cover is whatever
// the frame before left there. Compositing is what makes the animation look
// like the one the sender saw.
func TestDecodeGIFCompositesPartialFrames(t *testing.T) {
	palette := color.Palette{color.Black, color.White}

	background := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)

	patch := image.NewPaletted(image.Rect(1, 1, 3, 3), palette)
	for i := range patch.Pix {
		patch.Pix[i] = 1 // white, where black is 0
	}

	animation := &gif.GIF{
		Config: image.Config{ColorModel: palette, Width: 4, Height: 4},
		Image:  []*image.Paletted{background, patch},
		Delay:  []int{10, 10},

		// The first frame is cleared before the second: only the patch should
		// remain, over transparency.
		Disposal: []byte{gif.DisposalBackground, gif.DisposalNone},
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, animation); err != nil {
		t.Fatal(err)
	}

	clip, err := decodeVideo(writeVideo(t, "patch.gif", buf.Bytes()))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	if len(clip.frames) != 2 {
		t.Fatalf("%d frames, want 2", len(clip.frames))
	}

	// Outside the patch the first frame was cleared; inside it, the second
	// frame's colour is there.
	outside := color.NRGBAModel.Convert(clip.frames[1].At(0, 0)).(color.NRGBA)
	inside := color.NRGBAModel.Convert(clip.frames[1].At(2, 2)).(color.NRGBA)

	if outside.A != 0 {
		t.Errorf("the cleared area is %+v, want transparent", outside)
	}

	if inside == (color.NRGBA{}) {
		t.Errorf("the patch is %+v, want the frame's colour", inside)
	}
}

// waitFor plays the clip and waits until it finishes or the deadline passes.
func waitForVideo(t *testing.T, player *videoPlayer, clip *videoClip, from time.Duration, want int) []int {
	t.Helper()

	var (
		mu    sync.Mutex
		order []int
		done  = make(chan struct{})
		once  sync.Once
	)

	index := make(map[image.Image]int, len(clip.frames))
	for i, frame := range clip.frames {
		index[frame] = i
	}

	player.Play(clip, from, func(frame image.Image) {
		mu.Lock()
		order = append(order, index[frame])
		mu.Unlock()
	}, func() {
		once.Do(func() { close(done) })
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the animation never reached its end")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != want {
		t.Errorf("%d frames shown, want %d: %v", len(order), want, order)
	}

	return order
}

// The frames reach the surface in order, and the end is reported once.
func TestVideoPlaysEveryFrame(t *testing.T) {
	clip, err := decodeVideo(writeVideo(t, "anim.gif", testGIF(t, 3, 1)))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	order := waitForVideo(t, &videoPlayer{}, clip, 0, 3)

	if len(order) == 3 && (order[0] != 0 || order[1] != 1 || order[2] != 2) {
		t.Errorf("frames arrived as %v, want 0,1,2", order)
	}
}

// Playing from a point starts at the frame that covers it, which is what the
// seek bar means.
func TestVideoStartsAtTheChosenPoint(t *testing.T) {
	// Ten frames of 10 hundredths of a second: 100ms each is too slow for a
	// test, so build the delays directly.
	clip, err := decodeVideo(writeVideo(t, "ten.gif", testGIF(t, 10, 1)))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	// Each frame lasts 10ms after the zero-delay rule; ten frames is 100ms, so
	// 55ms lands in frame 5.
	order := waitForVideo(t, &videoPlayer{}, clip, 55*time.Millisecond, 5)

	if len(order) > 0 && order[0] != 5 {
		t.Errorf("playback started at frame %d, want 5", order[0])
	}
}

// Stop retires the run: no frame arrives after it, whatever the goroutine was
// waiting on.
func TestVideoStopStopsTheFrames(t *testing.T) {
	clip, err := decodeVideo(writeVideo(t, "anim.gif", testGIF(t, 20, 50)))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	player := &videoPlayer{}

	var (
		mu    sync.Mutex
		count int
	)

	player.Play(clip, 0, func(image.Image) {
		mu.Lock()
		count++
		mu.Unlock()
	}, func() {})

	time.Sleep(20 * time.Millisecond)
	player.Stop()

	mu.Lock()
	atStop := count
	mu.Unlock()

	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if count != atStop {
		t.Errorf("%d frames after Stop, want none (was %d)", count-atStop, atStop)
	}

	if player.Playing() {
		t.Error("the player still reports playing after Stop")
	}
}

// A seek while playing continues from the new point, and Position reports it.
func TestVideoSeekWhilePlaying(t *testing.T) {
	clip, err := decodeVideo(writeVideo(t, "ten.gif", testGIF(t, 10, 1)))
	if err != nil {
		t.Fatalf("decodeVideo() error = %v", err)
	}

	player := &videoPlayer{}

	var (
		mu    sync.Mutex
		order []int
	)

	index := make(map[image.Image]int, len(clip.frames))
	for i, frame := range clip.frames {
		index[frame] = i
	}

	player.Play(clip, 0, func(frame image.Image) {
		mu.Lock()
		order = append(order, index[frame])
		mu.Unlock()
	}, func() {})

	time.Sleep(15 * time.Millisecond)
	player.Seek(70 * time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	player.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(order) == 0 {
		t.Fatal("no frames were shown")
	}

	// After the seek, frames come from 7 onward.
	last := order[len(order)-1]
	if last < 7 {
		t.Errorf("the last frame after seeking to 70%% was %d, want 7 or later: %v", last, order)
	}
}

// The clock reads the position of the frame on screen and stops at the length.
func TestVideoPositionFollowsTheClock(t *testing.T) {
	clip := &videoClip{
		frames:   make([]image.Image, 3),
		delays:   []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond},
		duration: 150 * time.Millisecond,
	}

	player := &videoPlayer{}
	player.Play(clip, 100*time.Millisecond, func(image.Image) {}, func() {})

	if got := player.Position(); got < 100*time.Millisecond || got > 130*time.Millisecond {
		t.Errorf("position = %v, want about the point it started at", got)
	}

	player.Stop()

	if got := player.Position(); got != 0 {
		t.Errorf("position after Stop = %v, want the start", got)
	}
}

// frameAt is the arithmetic the seek and the clock share.
func TestClipFrameAt(t *testing.T) {
	clip := &videoClip{
		delays: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 50 * time.Millisecond},
	}

	tests := []struct {
		position time.Duration
		frame    int
		start    time.Duration
	}{
		{0, 0, 0},
		{50 * time.Millisecond, 0, 0},
		{100 * time.Millisecond, 1, 100 * time.Millisecond},
		{299 * time.Millisecond, 1, 100 * time.Millisecond},
		{300 * time.Millisecond, 2, 300 * time.Millisecond},
		{time.Hour, 2, 300 * time.Millisecond},
	}

	for _, tt := range tests {
		frame, start := clip.frameAt(tt.position)
		if frame != tt.frame || start != tt.start {
			t.Errorf("frameAt(%v) = %d at %v, want %d at %v", tt.position, frame, start, tt.frame, tt.start)
		}
	}
}

// The player refuses a clip it cannot show rather than panicking on it.
func TestVideoIgnoresAnEmptyClip(t *testing.T) {
	player := &videoPlayer{}
	player.Play(nil, 0, func(image.Image) {}, func() {})

	if player.Playing() {
		t.Error("an empty clip started playing")
	}
}
