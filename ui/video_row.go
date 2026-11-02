//go:build gui

package ui

import (
	"image"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// videoHeight is how tall the picture sits in the transcript, the same as a
// still picture: an animation is a picture that moves, not a cinema screen.
const videoHeight = inlineImageHeight

// videoRow is the player a file row carries when the file is an animation this
// build can decode: a surface, Play/Stop and a clock, and a seek bar (#421).
type videoRow struct {
	box     *fyne.Container
	surface *canvas.Image
	play    *widgets.CopyButton
	clock   *widgets.Text
	seek    *widget.Slider
}

func newVideoRow(g *gui) videoRow {
	surface := canvas.NewImageFromResource(nil)
	surface.FillMode = canvas.ImageFillContain
	surface.SetMinSize(fyne.NewSize(0, videoHeight))

	play := widgets.NewButtonWithIcon("Play", theme.MediaPlayIcon(), nil, g.copyLabel)
	clock := g.text("")

	seek := widget.NewSlider(0, 1)
	seek.Step = 0.01
	seek.Hide()

	row := videoRow{
		box:     container.NewVBox(surface, container.NewHBox(play, clock), seek),
		surface: surface,
		play:    play,
		clock:   clock,
		seek:    seek,
	}
	row.hide()

	return row
}

func videoRowOf(object fyne.CanvasObject) (videoRow, bool) {
	box, ok := object.(*fyne.Container)
	if !ok || len(box.Objects) < 3 {
		return videoRow{}, false
	}

	surface, ok := box.Objects[0].(*canvas.Image)
	if !ok {
		return videoRow{}, false
	}

	line, ok := box.Objects[1].(*fyne.Container)
	if !ok || len(line.Objects) < 2 {
		return videoRow{}, false
	}

	play, ok := line.Objects[0].(*widgets.CopyButton)
	if !ok {
		return videoRow{}, false
	}

	clock, ok := line.Objects[1].(*widgets.Text)
	if !ok {
		return videoRow{}, false
	}

	seek, ok := box.Objects[2].(*widget.Slider)
	if !ok {
		return videoRow{}, false
	}

	return videoRow{box: box, surface: surface, play: play, clock: clock, seek: seek}, true
}

func (r videoRow) hide() {
	if r.box != nil {
		r.box.Hide()
	}
}

func (r videoRow) show() {
	if r.box != nil {
		r.box.Show()
	}
}

// fillVideoRow shows the player on a row whose file this build can animate, and
// keeps it hidden everywhere else.
func (g *gui) fillVideoRow(m chatMessage, row videoRow) {
	if row.box == nil {
		return
	}

	if m.file == nil || m.file.path == "" || !playsHere(m.file.state) || !g.animatable(m.file.path) {
		row.hide()

		g.mu.Lock()
		if g.videoSurface == row.surface {
			g.videoSurface, g.videoClock = nil, nil
		}
		g.mu.Unlock()

		return
	}

	path := m.file.path
	clip, loaded := g.videoClipFor(path)
	playing := g.videoPlaying(path)

	row.play.SetOnTapped(func() { g.toggleVideo(path) })
	row.play.SetText("Play")
	row.play.SetIcon(theme.MediaPlayIcon())

	if playing {
		row.play.SetText("Stop")
		row.play.SetIcon(theme.MediaStopIcon())
	}

	// The surface belongs to the row showing this clip while it plays, so a
	// frame lands here and nowhere else. A repaint of a different row drops the
	// pointer, the way the audio clock's is dropped.
	g.mu.Lock()
	if playing {
		g.videoSurface, g.videoClock = row.surface, row.clock
	} else if g.videoSurface == row.surface {
		g.videoSurface, g.videoClock = nil, nil
	}

	frame := g.videoFrame
	g.mu.Unlock()

	// A frame belongs to the clip this window loaded, and to no other row: a
	// transcript with two animations must not show one inside the other.
	if !loaded {
		frame = nil
	}

	switch {
	case frame != nil:
		row.surface.Image = frame
		row.surface.Resource = nil
		row.surface.Refresh()

	case loaded && len(clip.frames) > 0:
		row.surface.Image = clip.frames[0]
		row.surface.Resource = nil
		row.surface.Refresh()
	}

	if loaded {
		position := g.video.Position()

		row.clock.SetText(videoLabel(position, clip.duration))
		row.seek.Show()
		row.seek.OnChanged = func(value float64) {
			if g.videoSyncing {
				return
			}

			g.seekVideo(path, value)
		}

		g.videoSyncing = true
		row.seek.SetValue(fractionOf(position, clip.duration))
		g.videoSyncing = false
	} else {
		row.clock.SetText("")
		row.seek.Hide()
	}

	row.show()
}

// fractionOf is a position over a length, clamped for a slider.
func fractionOf(position, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}

	value := float64(position) / float64(duration)
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}

// animatable is whether this build can decode the file at path, asked once per
// path from the header — the picture cache's rule applied to animations (#421).
// A GIF that turns out to be a still picture is remembered as not animatable,
// so its row stops offering a Play that cannot work.
func (g *gui) animatable(path string) bool {
	if ok, known := g.videoFiles[path]; known {
		return ok
	}

	ok := canPlayVideo(path)

	if g.videoFiles == nil {
		g.videoFiles = make(map[string]bool)
	}

	if len(g.videoFiles) >= previewCacheMax {
		clear(g.videoFiles)
	}

	g.videoFiles[path] = ok

	return ok
}

// notAnimatable remembers that a file this build thought it could animate is
// not one, so the row gives the picture back to the still preview.
func (g *gui) notAnimatable(path string) {
	if g.videoFiles == nil {
		g.videoFiles = make(map[string]bool)
	}

	g.videoFiles[path] = false
}

// toggleVideo plays the animation at path from where it was left, or stops it.
func (g *gui) toggleVideo(path string) {
	if g.videoPlaying(path) {
		g.video.Pause()
		g.chatList.Refresh()

		return
	}

	if clip, loaded := g.videoClipFor(path); loaded {
		g.startVideo(path, clip, g.video.Position())

		return
	}

	g.setStatus("Opening " + filepath.Base(path) + "…")

	go func() {
		clip, err := decodeVideo(path)

		fyne.Do(func() {
			if err != nil {
				g.notAnimatable(path)
				g.setStatus("Cannot play " + filepath.Base(path) + ": " + err.Error())
				g.chatList.Refresh()

				return
			}

			g.loadVideo(path, clip)
			g.startVideo(path, clip, 0)
		})
	}()
}

// startVideo plays the loaded clip, putting each frame on the surface.
func (g *gui) startVideo(path string, clip *videoClip, from time.Duration) {
	g.mu.Lock()
	g.videoPath = path
	g.mu.Unlock()

	g.video.Play(clip, from, func(frame image.Image) {
		fyne.Do(func() { g.showVideoFrame(path, frame) })
	}, func() {
		fyne.Do(func() { g.chatList.Refresh() })
	})

	g.chatList.Refresh()

	go g.tickVideo(path)
}

// showVideoFrame puts one frame on the row that is showing path.
func (g *gui) showVideoFrame(path string, frame image.Image) {
	g.mu.Lock()
	if g.videoPath != path {
		g.mu.Unlock()

		return
	}

	g.videoFrame = frame
	surface := g.videoSurface
	g.mu.Unlock()

	if surface == nil {
		return
	}

	surface.Image = frame
	surface.Resource = nil
	surface.Refresh()
}

// tickVideo keeps the clock moving while the picture is.
func (g *gui) tickVideo(path string) {
	ticker := time.NewTicker(audioPollInterval)
	defer ticker.Stop()

	for range ticker.C {
		if !g.videoPlaying(path) {
			return
		}

		clip, loaded := g.videoClipFor(path)
		if !loaded {
			return
		}

		position := g.video.Position()

		fyne.Do(func() {
			g.mu.Lock()
			clock := g.videoClock
			g.mu.Unlock()

			if clock != nil {
				clock.SetText(videoLabel(position, clip.duration))
			}
		})
	}
}

// videoPlaying reports whether the animation at path is moving.
func (g *gui) videoPlaying(path string) bool {
	g.mu.Lock()
	current := g.videoPath
	g.mu.Unlock()

	return current == path && g.video.Playing()
}

// videoClipFor is the clip this window decoded for path, if it is the one
// loaded. Only one is kept: an animation is large, and a transcript that
// scrolled through ten of them would hold all ten.
func (g *gui) videoClipFor(path string) (*videoClip, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.videoClip == nil || g.videoClipPath != path {
		return nil, false
	}

	return g.videoClip, true
}

// loadVideo remembers a decoded clip, replacing whatever was loaded.
func (g *gui) loadVideo(path string, clip *videoClip) {
	g.mu.Lock()
	g.videoClip, g.videoClipPath = clip, path
	g.videoFrame = nil
	g.mu.Unlock()

	g.video.Stop()
}

// seekVideo moves through the animation at path.
func (g *gui) seekVideo(path string, fraction float64) {
	clip, loaded := g.videoClipFor(path)
	if !loaded || clip.duration <= 0 {
		return
	}

	g.video.Seek(time.Duration(float64(clip.duration) * fraction))
	g.chatList.Refresh()
}
