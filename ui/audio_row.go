//go:build gui

package ui

import (
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// decodeClip is the decoder the player and the waveform both use. It is a
// variable so a test can count the decodes: preparing a waveform and then
// pressing Play must be one decode, not two (#435).
var decodeClip = decodeAudio

// audioPollInterval is how often a playing row's clock moves. The device
// reports no playhead, so the window asks the engine where it is; four times a
// second is enough to look like a clock and cheap enough to leave running.
const audioPollInterval = 250 * time.Millisecond

// audioRow is the player a file row carries: a Play/Stop button and the clock
// that shows the length, or how far into the clip the sound has got (#306).
//
// It lives on the row rather than in a window of its own: the file is already
// in the conversation, and a second window for a few seconds of speech is more
// to close than to hear.
type audioRow struct {
	box   *fyne.Container
	play  *widgets.CopyButton
	clock *widgets.Text
	wave  *widgets.Waveform
}

// newAudioRow builds the player a row carries, hidden until a row needs it.
//
// The waveform is under the button and the clock: a row can be walked through a
// clip by dragging it, which is the one thing the buttons cannot do (#399).
func newAudioRow(g *gui) audioRow {
	play := widgets.NewButtonWithIcon("Play", theme.MediaPlayIcon(), nil, g.copyLabel)
	clock := g.text("")
	wave := widgets.NewWaveform(nil)
	wave.Hide()

	row := audioRow{
		box:   container.NewVBox(container.NewHBox(play, clock), wave),
		play:  play,
		clock: clock,
		wave:  wave,
	}
	row.hide()

	return row
}

// audioRowOf finds the player inside a row's template.
func audioRowOf(object fyne.CanvasObject) (audioRow, bool) {
	box, ok := object.(*fyne.Container)
	if !ok || len(box.Objects) < 2 {
		return audioRow{}, false
	}

	top, ok := box.Objects[0].(*fyne.Container)
	if !ok || len(top.Objects) < 2 {
		return audioRow{}, false
	}

	play, ok := top.Objects[0].(*widgets.CopyButton)
	if !ok {
		return audioRow{}, false
	}

	clock, ok := top.Objects[1].(*widgets.Text)
	if !ok {
		return audioRow{}, false
	}

	wave, ok := box.Objects[1].(*widgets.Waveform)
	if !ok {
		return audioRow{}, false
	}

	return audioRow{box: box, play: play, clock: clock, wave: wave}, true
}

func (r audioRow) hide() {
	if r.box != nil {
		r.box.Hide()
	}
}

func (r audioRow) show() {
	if r.box != nil {
		r.box.Show()
	}
}

// audioLabel is the clock beside the play button: how far into the clip the
// sound has got over the whole length, or just the length when it is not
// playing. A clip this window has never decoded shows nothing rather than a
// duration it would have to decode to know.
func audioLabel(position, duration time.Duration) string {
	if duration <= 0 {
		return ""
	}

	if position <= 0 {
		return audioClock(duration)
	}

	return audioClock(position) + " / " + audioClock(duration)
}

// fillAudioRow offers the player on a row whose file this build can decode, and
// keeps it hidden everywhere else.
//
// A row with no path on disk has nothing to decode — a transfer still moving, a
// failed one, or a send from a phone whose picker handed over a content:// URI
// (#303) — and a format with no decoder here (Ogg, AAC, FLAC) keeps the actions
// it already had. Only WAV and MP3 play in this build (#306).
func (g *gui) fillAudioRow(m chatMessage, row audioRow) {
	if row.box == nil {
		return
	}

	if m.file == nil || m.file.path == "" || !playsHere(m.file.state) || !g.playable(m.file.path) {
		row.hide()

		return
	}

	path := m.file.path

	// The wave is the control, so it has to be there before Play: the first
	// paint of an audio row decodes the clip in the background, once (#435).
	g.prepareWaveform(path)

	row.play.SetOnTapped(func() { g.toggleAudio(path) })

	if g.audioPlaying(path) {
		duration, position := g.audioShownProgress(path)

		row.play.SetText("Stop")
		row.play.SetIcon(theme.MediaStopIcon())
		row.clock.SetText(audioLabel(position, duration))

		// The clock moves under the ticker, and the list may reuse this widget
		// for another message before this row is painted again. Binding is the
		// one place a widget changes which file it shows, so the ticker's
		// pointer is kept current here and dropped by the row that loses it.
		g.mu.Lock()
		g.audioClock = row.clock
		g.mu.Unlock()

		g.fillWaveform(row, path, g.audioFraction())

		row.show()

		return
	}

	row.play.SetText("Play")
	row.play.SetIcon(theme.MediaPlayIcon())
	row.clock.SetText(audioLabel(0, g.audioDurationFor(path)))

	// This widget no longer shows the clip that is sounding, whatever it showed
	// before it was bound to this row.
	g.mu.Lock()
	if g.audioClock == row.clock {
		g.audioClock = nil
	}
	g.mu.Unlock()

	g.fillWaveform(row, path, float32(g.audioResume(path)))

	row.show()
}

// fillWaveform gives the row's seek control the clip's shape, where the sound
// has got to, and the handler that moves it. A clip this window has not decoded
// has no shape yet, so the control stays hidden rather than drawing an empty
// box (#399).
func (g *gui) fillWaveform(row audioRow, path string, fraction float32) {
	peaks := g.audioPeaksFor(path)
	if len(peaks) == 0 {
		row.wave.Hide()

		return
	}

	row.wave.SetPeaks(peaks)
	row.wave.SetFraction(fraction)
	row.wave.SetOnSeek(func(chosen float32) { g.seekAudio(path, float64(chosen)) })
	row.wave.Show()
}

// playsHere reports whether a row in this state holds a whole file on disk: a
// finished arrival, a finished send, or an arrival waiting for the user to pick
// where it goes — all three are decodable. A transfer still moving has only the
// part it has written so far.
func playsHere(state transferState) bool {
	switch state {
	case transferSaved, transferSent, transferPending:
		return true
	}

	return false
}

// toggleAudio starts the clip at path, or silences it if it is the one playing.
//
// Play resumes where Stop left the playhead: a seek bar that forgets the point
// every time the sound is paused is half a seek bar (#409).
func (g *gui) toggleAudio(path string) {
	if g.audioPlaying(path) {
		g.stopAudio()
		g.chatList.Refresh()

		return
	}

	g.startAudio(path, g.audioResume(path))
}

// seekAudio moves through the clip at path: while it plays, playback continues
// from the chosen point; while it is idle, it starts there (#399).
func (g *gui) seekAudio(path string, fraction float64) {
	if !g.audioPlaying(path) {
		g.startAudio(path, fraction)

		return
	}

	duration, _ := g.audioProgress()
	if duration <= 0 {
		return
	}

	if fraction < 0 {
		fraction = 0
	}

	if fraction > 1 {
		fraction = 1
	}

	if err := g.player().Seek(time.Duration(float64(duration) * fraction)); err != nil {
		g.setStatus("Cannot move through " + filepath.Base(path) + ": " + err.Error())

		return
	}

	if g.chatList != nil {
		g.chatList.Refresh()
	}
}

// audioFraction is how far into the playing clip the sound has got, 0..1.
func (g *gui) audioFraction() float32 {
	duration, position := g.audioProgress()
	if duration <= 0 || position <= 0 {
		return 0
	}

	return float32(position) / float32(duration)
}

// audioPeaksFor is the clip's shape, when this window has decoded it.
func (g *gui) audioPeaksFor(path string) []float32 {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.audioPeaks[path]
}

// startAudio decodes path beside the UI and hands the clip to the device.
//
// One clip at a time: the device belongs to the window, and two sounds at once
// is not a conversation. Decoding happens off the UI goroutine because a
// three-minute clip is real work; the row says Stop as soon as Play is pressed,
// so the tap has an answer before the decode finishes.
func (g *gui) startAudio(path string, from float64) {
	g.stopAudio()

	g.mu.Lock()
	g.audioGen++
	gen := g.audioGen
	g.audioPath = path
	g.mu.Unlock()

	g.chatList.Refresh()

	g.mu.Lock()
	cached, cachedPath := g.audioClip, g.audioClipPath
	g.mu.Unlock()

	go func() {
		clip, err := cached, error(nil)

		// A clip the waveform prepared is the clip Play wants: decoding it
		// again to hear what was just drawn would be the same file twice
		// (#435).
		if clip == nil || cachedPath != path {
			clip, err = decodeClip(path)
		}

		var peaks []float32
		if err == nil {
			peaks = audioPeaks(clip.pcm, audioPeakColumns)
		}

		fyne.Do(func() {
			g.mu.Lock()
			current := g.audioGen == gen
			g.mu.Unlock()

			// A Stop, or a newer Play, has already moved on: this decode has
			// nothing left to do.
			if !current {
				return
			}

			if err != nil {
				g.endAudio(gen)
				g.setStatus("Cannot play " + filepath.Base(path) + ": " + err.Error())

				return
			}

			g.mu.Lock()
			g.audioDuration = clip.duration

			if len(peaks) > 0 {
				g.rememberClip(path, clip, peaks)
			}
			g.mu.Unlock()

			// Play runs on the UI goroutine with Stop, so the two cannot be
			// reordered: a Stop that lands while a clip is being decoded
			// cannot be overtaken by a Play that was already on its way.
			// Opening the device costs a moment on the first clip only.
			if err := g.player().Play(clip, func(error) {
				fyne.Do(func() { g.endAudio(gen) })
			}); err != nil {
				g.endAudio(gen)
				g.setStatus("Cannot play " + filepath.Base(path) + ": " + err.Error())

				return
			}

			if from > 0 {
				_ = g.audio.Seek(time.Duration(float64(clip.duration) * from))
			}

			go g.tickAudio(gen)

			g.chatList.Refresh()
		})
	}()
}

// prepareWaveform decodes a clip in the background the first time its row is
// painted, so the waveform is there before Play and a tap can choose a point
// without starting the sound first (#435).
//
// One decode at a time: a transcript can show several audio rows at once, and
// decoding all of them at once would spend the machine on clips nobody asked to
// hear. A row that was skipped gets its turn on the next paint.
func (g *gui) prepareWaveform(path string) {
	g.mu.Lock()

	if g.audioPrepared[path] || g.audioPreparing {
		g.mu.Unlock()

		return
	}

	g.audioPreparing = true
	g.mu.Unlock()

	go func() {
		clip, err := decodeClip(path)

		g.mu.Lock()
		g.audioPreparing = false

		if err == nil {
			g.rememberClip(path, clip, audioPeaks(clip.pcm, audioPeakColumns))
		}

		g.mu.Unlock()

		// No repaint from here: a background goroutine that paints is a paint
		// from a second thread, and the row's next paint — a scroll, a message,
		// the layout settling — picks the waveform up. What the row shows is
		// read from the caches, so nothing else is needed to make it appear.
	}()
}

// rememberClip records what a decode learned about a clip. The caller holds the
// lock.
func (g *gui) rememberClip(path string, clip *audioClip, peaks []float32) {
	if g.audioDurations == nil {
		g.audioDurations = make(map[string]time.Duration)
	}

	if g.audioPeaks == nil {
		g.audioPeaks = make(map[string][]float32)
	}

	if g.audioPrepared == nil {
		g.audioPrepared = make(map[string]bool)
	}

	if len(g.audioDurations) >= previewCacheMax {
		clear(g.audioDurations)
		clear(g.audioPeaks)
		clear(g.audioPrepared)
	}

	g.audioDurations[path] = clip.duration
	g.audioPeaks[path] = peaks
	g.audioPrepared[path] = true

	// One slot: the clip a Play is most likely to want next.
	g.audioClip, g.audioClipPath = clip, path
}

// audioShownProgress is what the row draws while the sound is starting: the
// live position once the device has it, and the remembered one until then, so a
// resumed clip does not flash back to the start (#435).
func (g *gui) audioShownProgress(path string) (time.Duration, time.Duration) {
	duration, position := g.audioProgress()
	if duration > 0 {
		return duration, position
	}

	known := g.audioDurationFor(path)
	if known <= 0 {
		return duration, position
	}

	return known, time.Duration(float64(known) * g.audioResume(path))
}

// stopAudio silences the clip that is sounding, keeping the playhead where the
// sound had got to, so Play resumes there (#409).
//
// It touches the engine and the window's state, and no widget: the window's
// close has no event loop to repaint on, and a caller that wants the rows to
// say Play again asks for the refresh.
func (g *gui) stopAudio() {
	g.mu.Lock()
	engine := g.audio
	path := g.audioPath
	duration := g.audioDuration
	g.audioPath = ""
	g.audioDuration = 0
	g.audioClock = nil
	g.audioGen++
	g.mu.Unlock()

	if engine == nil || path == "" {
		return
	}

	// The position is read before the device is stopped: a stopped player has
	// no playhead to ask.
	position := engine.Position()
	engine.Stop()

	if duration > 0 && position > 0 {
		g.mu.Lock()
		if g.audioResumeAt == nil {
			g.audioResumeAt = make(map[string]float64)
		}

		g.audioResumeAt[path] = float64(position) / float64(duration)
		g.mu.Unlock()
	}
}

// audioResume is where Play should continue from: the playhead Stop left, or
// the start for a clip that has not been stopped or has played to its end.
func (g *gui) audioResume(path string) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.audioResumeAt[path]
}

// endAudio retires the clip gen started, if it is still the one playing.
//
// It is what the engine calls when the sound reaches its end, and what a failed
// decode calls: either way the row goes back to offering Play.
func (g *gui) endAudio(gen int) {
	g.mu.Lock()
	if g.audioGen != gen {
		g.mu.Unlock()

		return
	}

	// A clip that reached its end starts over: "resuming" at its last
	// millisecond would play nothing.
	path := g.audioPath
	g.audioPath = ""
	g.audioDuration = 0
	g.audioClock = nil
	g.audioGen++
	delete(g.audioResumeAt, path)
	g.mu.Unlock()

	if g.chatList != nil {
		g.chatList.Refresh()
	}
}

// tickAudio moves the playing row's clock until the clip is retired.
func (g *gui) tickAudio(gen int) {
	ticker := time.NewTicker(audioPollInterval)
	defer ticker.Stop()

	for range ticker.C {
		g.mu.Lock()
		if g.audioGen != gen || g.audioPath == "" {
			g.mu.Unlock()

			return
		}

		engine := g.audio
		clock := g.audioClock
		duration := g.audioDuration
		g.mu.Unlock()

		if engine == nil || clock == nil {
			continue
		}

		position := engine.Position()

		fyne.Do(func() { clock.SetText(audioLabel(position, duration)) })
	}
}

// audioPlaying reports whether path is the clip the device is sounding.
func (g *gui) audioPlaying(path string) bool {
	if path == "" {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	return g.audioPath == path
}

// audioProgress is the playing clip's length and how far into it the sound has
// got. The engine's clock is asked outside the window's lock, because the
// engine has one of its own.
func (g *gui) audioProgress() (time.Duration, time.Duration) {
	g.mu.Lock()
	engine, duration := g.audio, g.audioDuration
	g.mu.Unlock()

	var position time.Duration
	if engine != nil {
		position = engine.Position()
	}

	return duration, position
}

// player is the window's sound device, made on first use: a window that never
// plays anything never touches the sound card, and a machine with no audio at
// all still gets a window.
func (g *gui) player() audioEngine {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.audio == nil {
		g.audio = audioEngineFor()
	}

	return g.audio
}

// playable is whether this build has a decoder for the file at path, asked once
// per path: the answer comes from the header, and a repaint must not open the
// file again.
func (g *gui) playable(path string) bool {
	g.cacheMu.Lock()
	ok, known := g.playableFiles[path]
	g.cacheMu.Unlock()

	if known {
		return ok
	}

	ok = canPlayAudio(path)

	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()

	if g.playableFiles == nil {
		g.playableFiles = make(map[string]bool)
	}

	if len(g.playableFiles) >= previewCacheMax {
		clear(g.playableFiles)
	}

	g.playableFiles[path] = ok

	return ok
}

// audioDurationFor is how long a clip is, when this window has decoded it
// before. A file that has never been played shows no duration rather than
// decoding itself on a repaint.
func (g *gui) audioDurationFor(path string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.audioDurations[path]
}
