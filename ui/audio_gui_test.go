//go:build gui

package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

// fakeAudioEngine is the window's sound device, recorded instead of opened: the
// headless gate has no speaker, and what is being checked here is what the row
// asks the device to do (#306).
type fakeAudioEngine struct {
	mu       sync.Mutex
	plays    int
	stops    int
	seeks    []time.Duration
	clip     *audioClip
	onDone   func(error)
	position time.Duration
	playErr  error
}

func (f *fakeAudioEngine) Play(clip *audioClip, onDone func(error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.playErr != nil {
		return f.playErr
	}

	f.plays++
	f.clip = clip
	f.onDone = onDone

	return nil
}

// Seek records the move and makes Position report from there, the way a device
// that started playing at the new point would.
func (f *fakeAudioEngine) Seek(position time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.playErr != nil {
		return f.playErr
	}

	f.seeks = append(f.seeks, position)
	f.position = position

	return nil
}

func (f *fakeAudioEngine) seekCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.seeks)
}

func (f *fakeAudioEngine) lastSeek() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.seeks) == 0 {
		return -1
	}

	return f.seeks[len(f.seeks)-1]
}

func (f *fakeAudioEngine) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.stops++
	f.onDone = nil
}

func (f *fakeAudioEngine) Position() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.position
}

func (f *fakeAudioEngine) setPosition(position time.Duration) {
	f.mu.Lock()
	f.position = position
	f.mu.Unlock()
}

func (f *fakeAudioEngine) counts() (plays, stops int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.plays, f.stops
}

// done is the engine reaching the end of the clip.
func (f *fakeAudioEngine) done(err error) {
	f.mu.Lock()
	onDone := f.onDone
	f.mu.Unlock()

	if onDone != nil {
		onDone(err)
	}
}

// installAudioEngine hands the window a device that records instead of
// sounding, and puts the real one back for the next test.
func installAudioEngine(t *testing.T, engine audioEngine) {
	t.Helper()

	previous := audioEngineFor
	audioEngineFor = func() audioEngine { return engine }

	t.Cleanup(func() { audioEngineFor = previous })
}

// newAudioTestGUI builds a window and silences it when the test ends: a clip
// left playing keeps the clock's ticker (and the fake device) alive past the
// app, which is a goroutine leak the test would be hiding.
func newAudioTestGUI(t *testing.T) *gui {
	t.Helper()

	g := newTestGUI(t)
	t.Cleanup(g.stopAudio)

	return g
}

// audioNote is a saved clip row on disk, the way a received or sent file leaves
// it.
func audioNote(t *testing.T, name string, body []byte) *fileNote {
	t.Helper()

	path := writeTestAudio(t, name, body)

	return &fileNote{id: "id-1", name: name, size: int64(len(body)), state: transferSaved, path: path}
}

// paintRow paints one transcript row and returns the player it carries.
func paintRow(t *testing.T, g *gui, id int) audioRow {
	t.Helper()

	row := g.chatList.CreateItem()
	g.updateChatRow(id, row)

	return audioRowObjects(t, row)
}

// A clip this build can decode offers its player on the row: the button says
// Play, and the clock has nothing to say until the file has been decoded once.
func TestTheAudioRowOffersPlay(t *testing.T) {
	g := newAudioTestGUI(t)
	g.messages = []chatMessage{{from: "laptop", file: audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 800))}}

	audio := paintRow(t, g, 0)

	if !audio.box.Visible() {
		t.Fatal("a WAV row hid its player")
	}

	if got := audio.play.Text(); got != "Play" {
		t.Errorf("the button says %q, want Play", got)
	}

	if got := audio.clock.Text(); got != "" {
		t.Errorf("the clock says %q before the clip was decoded, want nothing", got)
	}
}

// An arrival that is waiting for the user to pick a folder is a whole file on
// disk, so it plays like a saved one — a phone's default policy asks, and a
// dismissed dialog must not take the player away.
func TestAnArrivalWaitingToBeSavedStillPlays(t *testing.T) {
	g := newAudioTestGUI(t)

	note := audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 800))
	note.state = transferPending
	g.messages = []chatMessage{{from: "laptop", file: note}}

	if !paintRow(t, g, 0).box.Visible() {
		t.Error("a clip that arrived and is waiting to be saved hid its player")
	}
}

// A row that has nothing of its own to play keeps the actions it had: a PDF, a
// format this build has no decoder for, and a file that is not on this disk.
func TestAFileThatCannotPlayHasNoPlayer(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		note *fileNote
	}{
		{"a PDF", &fileNote{name: "report.pdf", size: 12, state: transferSaved, path: filepath.Join(dir, "report.pdf")}},
		{"an Ogg clip", &fileNote{name: "voice.ogg", size: 64, state: transferSaved, path: filepath.Join(dir, "voice.ogg")}},
		{"a transfer still moving", &fileNote{name: "memo.wav", size: 100, state: transferSending, path: filepath.Join(dir, "moving.wav")}},
		{"a phone's send with no path", &fileNote{name: "memo.wav", size: 100, state: transferSent}},
		{"a text row", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newAudioTestGUI(t)
			g.messages = []chatMessage{{from: "laptop", file: tt.note}}

			audio := paintRow(t, g, 0)

			if audio.box.Visible() {
				t.Error("the row offered a player it cannot honour")
			}
		})
	}
}

// Pressing Play decodes the clip beside the UI and hands it to the device.
func TestPlayDecodesAndStartsTheClip(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 8000))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to reach the device", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	if !g.audioPlaying(note.path) {
		t.Error("the window does not report the clip as playing")
	}

	// The decode happened off the UI goroutine; let its repaint settle before
	// reading the widgets.
	time.Sleep(20 * time.Millisecond)

	engine.setPosition(3 * time.Second)
	audio = paintRow(t, g, 0)

	if got := audio.play.Text(); got != "Stop" {
		t.Errorf("the button says %q while the clip plays, want Stop", got)
	}

	if got := audio.clock.Text(); !strings.Contains(got, " / ") {
		t.Errorf("the clock says %q, want the position over the length", got)
	}
}

// Tapping Stop silences the clip and puts the row back the way it was.
func TestStopFromTheRowSilencesTheClip(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 800))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	time.Sleep(20 * time.Millisecond)

	audio = paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	if _, stops := engine.counts(); stops != 1 {
		t.Errorf("Stop reached the device %d times, want 1", stops)
	}

	waitForCondition(t, "the window to forget the clip", func() bool { return !g.audioPlaying(note.path) })

	if got := paintRow(t, g, 0).play.Text(); got != "Play" {
		t.Errorf("the button says %q after Stop, want Play", got)
	}
}

// One clip at a time: starting a second one silences the first.
func TestASecondClipSilencesTheFirst(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	first := audioNote(t, "first.wav", testWAV(t, 8000, 1, 16, 800))
	second := audioNote(t, "second.wav", testWAV(t, 8000, 1, 16, 800))
	g.messages = []chatMessage{{file: first}, {file: second}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the first clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	audio = paintRow(t, g, 1)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the second clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 2
	})

	if _, stops := engine.counts(); stops != 1 {
		t.Errorf("the first clip was stopped %d times, want 1", stops)
	}

	if g.audioPlaying(first.path) || !g.audioPlaying(second.path) {
		t.Error("the window is playing the wrong clip")
	}
}

// The end of a clip puts the row back to Play, with its length on the clock.
func TestTheEndOfAClipOffersPlayAgain(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	engine.done(nil)

	waitForCondition(t, "the clip to be retired", func() bool { return !g.audioPlaying(note.path) })

	time.Sleep(20 * time.Millisecond)

	audio = paintRow(t, g, 0)

	if got := audio.play.Text(); got != "Play" {
		t.Errorf("the button says %q after the clip ended, want Play", got)
	}

	// The length is remembered from the decode, so the row shows it without
	// opening the file again.
	if got := audio.clock.Text(); got != audioClock(time.Second) {
		t.Errorf("the clock says %q after the clip ended, want %q", got, audioClock(time.Second))
	}
}

// A device that refuses the clip is reported and leaves the row at Play, not at
// a Stop button that stops nothing.
func TestADeviceThatRefusesTheClipLeavesTheRowAtPlay(t *testing.T) {
	engine := &fakeAudioEngine{playErr: errors.New("no audio device")}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 800))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the window to give up on the clip", func() bool { return !g.audioPlaying(note.path) })

	time.Sleep(20 * time.Millisecond)

	if got := paintRow(t, g, 0).play.Text(); got != "Play" {
		t.Errorf("the button says %q after the device refused, want Play", got)
	}

	if got := g.status.Text(); !strings.Contains(got, "Cannot play") {
		t.Errorf("the status line says %q, want the reason it could not play", got)
	}
}

// Stopping the connection stops the sound, and the window closing releases the
// device: a clip is not worth a listener left bound or a process that will not
// exit.
func TestStopAndShutdownSilenceThePlayer(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, 8000, 1, 16, 800))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	g.stop()

	if _, stops := engine.counts(); stops != 1 {
		t.Errorf("Stop silenced the device %d times, want 1", stops)
	}

	if g.audioPlaying(note.path) {
		t.Error("the window still reports the clip as playing after Stop")
	}

	g.shutdown()

	if _, stops := engine.counts(); stops != 1 {
		t.Errorf("the window closing stopped an idle device (%d stops), want no call", stops)
	}
}

// The window does not open the sound card until something is played: a user who
// never plays a clip never gives the app a reason to touch it.
func TestThePlayerIsMadeOnlyWhenSomethingPlays(t *testing.T) {
	g := newAudioTestGUI(t)

	if g.audio != nil {
		t.Fatal("the window opened a sound device while being built")
	}
}

// The clock moves while the clip plays: the device has no playhead, so the
// window asks it where it is and writes the answer on the row.
func TestTheClockMovesWhileTheClipPlays(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	note := audioNote(t, "memo.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate))
	g.messages = []chatMessage{{file: note}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	time.Sleep(20 * time.Millisecond)

	// The row must be bound to the playing clip for the ticker to have a widget
	// to write to.
	audio = paintRow(t, g, 0)

	engine.setPosition(10 * time.Second)

	waitForCondition(t, "the clock to move", func() bool {
		return strings.Contains(audio.clock.Text(), " / ")
	})
}

// The waveform appears when the clip has been decoded — before that there is no
// shape to draw, and the row shows what it always did (#399).
func TestTheWaveformAppearsOnceTheClipIsDecoded(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	g.messages = []chatMessage{{file: audioNote(t, "memo.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate))}}

	audio := paintRow(t, g, 0)

	if audio.wave.Visible() {
		t.Error("the waveform is drawn before the clip was decoded")
	}

	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	time.Sleep(20 * time.Millisecond)

	audio = paintRow(t, g, 0)

	if !audio.wave.Visible() {
		t.Fatal("no waveform after the decode")
	}

	if len(audio.wave.Peaks()) == 0 {
		t.Error("the waveform has no peaks")
	}
}

// Tapping the shape moves the sound: playback continues from the chosen point,
// and the device is told where (#399).
func TestSeekingMovesThePlayhead(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	g.messages = []chatMessage{{file: audioNote(t, "memo.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate))}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	time.Sleep(20 * time.Millisecond)

	audio = paintRow(t, g, 0)
	audio.wave.Resize(fyne.NewSize(200, 40))
	audio.wave.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 20)})

	waitForCondition(t, "the seek to reach the device", func() bool { return engine.seekCount() == 1 })

	// The clip is one second of 48 kHz stereo, so half of it is half a second.
	if got := engine.lastSeek(); got < 450*time.Millisecond || got > 550*time.Millisecond {
		t.Errorf("sought to %v, want about half of the one-second clip", got)
	}
}

// An idle clip tapped at a point starts there, which is what "play from this
// moment" means (#399).
func TestSeekingAnIdleClipStartsItThere(t *testing.T) {
	engine := &fakeAudioEngine{}
	installAudioEngine(t, engine)

	g := newAudioTestGUI(t)
	g.messages = []chatMessage{{file: audioNote(t, "memo.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate))}}

	audio := paintRow(t, g, 0)
	audio.play.Tapped(&fyne.PointEvent{})

	waitForCondition(t, "the clip to start", func() bool {
		plays, _ := engine.counts()

		return plays == 1
	})

	time.Sleep(20 * time.Millisecond)

	g.stopAudio()
	audio = paintRow(t, g, 0)
	audio.wave.Resize(fyne.NewSize(200, 40))
	audio.wave.Tapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})

	waitForCondition(t, "the clip to start again", func() bool {
		plays, _ := engine.counts()

		return plays == 2
	})

	waitForCondition(t, "the start point to reach the device", func() bool { return engine.seekCount() >= 1 })

	if got := engine.lastSeek(); got < 700*time.Millisecond || got > 800*time.Millisecond {
		t.Errorf("started at %v, want about three quarters of the one-second clip", got)
	}
}
