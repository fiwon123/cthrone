//go:build gui

package ui

import (
	"bytes"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// audioEngineFor makes the window's sound device. It is a variable for the same
// reason the transport dials are (#326): the headless gate has no speaker, so a
// test hands the window an engine that records what it was asked to play
// instead of opening a device (#306).
var audioEngineFor = func() audioEngine { return &otoEngine{} }

// otoEngine plays one clip at a time through oto.
//
// The device is opened on the first Play, not when the window is built: a user
// who never plays a sound never gives the app a reason to touch the sound card,
// and a machine with no audio at all still gets a window.
type otoEngine struct {
	mu sync.Mutex

	once   sync.Once
	ctx    *oto.Context
	ctxErr error

	// player is the clip that is sounding, and gen tells a poller that belongs
	// to an earlier clip that its end is no longer news.
	player   *oto.Player
	gen      int
	started  time.Time
	duration time.Duration
}

// context opens the device once. The second return of oto.NewContext is closed
// when the device is ready; a failure to open one is kept and returned by every
// later Play rather than retried forever.
func (e *otoEngine) context() (*oto.Context, error) {
	e.once.Do(func() {
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:   audioSampleRate,
			ChannelCount: audioChannels,
			Format:       oto.FormatSignedInt16LE,
		})
		if err != nil {
			e.ctxErr = err

			return
		}

		<-ready

		if err := ctx.Err(); err != nil {
			e.ctxErr = err

			return
		}

		e.ctx = ctx
	})

	return e.ctx, e.ctxErr
}

// Play starts clip, replacing whatever was sounding.
func (e *otoEngine) Play(clip *audioClip, onDone func(error)) error {
	if clip == nil || len(clip.pcm) == 0 {
		return errAudioUnsupported
	}

	ctx, err := e.context()
	if err != nil {
		return err
	}

	e.Stop()

	player := ctx.NewPlayer(bytes.NewReader(clip.pcm))

	e.mu.Lock()
	e.player = player
	e.gen++
	gen := e.gen
	e.started = time.Now()
	e.duration = clip.duration
	e.mu.Unlock()

	player.Play()

	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if player.IsPlaying() {
				continue
			}

			// A player that stopped because the user pressed Stop is not an
			// end to report: the window already moved on.
			if e.finish(gen, player) && onDone != nil {
				onDone(nil)
			}

			return
		}
	}()

	return nil
}

// finish retires player if it is still the one gen started, and says whether it
// was: a Stop already retired it, and the end it produced is not news.
func (e *otoEngine) finish(gen int, player *oto.Player) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.gen != gen || e.player != player {
		return false
	}

	e.player = nil

	return true
}

// Stop silences the clip that is sounding, if any.
//
// oto v3 closes a player when it becomes unreachable — Close is a no-op — so a
// pause is what silences the sound now, and dropping the reference lets the
// device reclaim the buffer. PauseAndStopReading is the stronger of the two: a
// paused player that still reads would keep decoding a clip nobody hears.
func (e *otoEngine) Stop() {
	e.mu.Lock()
	e.gen++
	player := e.player
	e.player = nil
	e.mu.Unlock()

	if player != nil {
		player.PauseAndStopReading()
	}
}

// Position is how far into the clip playback has got.
//
// The device does not report a playhead, so the position is the time since the
// clip started, capped at its duration. A clock on a chat row needs to move,
// not to be sample-accurate.
func (e *otoEngine) Position() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.player == nil {
		return 0
	}

	pos := time.Since(e.started)
	if pos > e.duration {
		pos = e.duration
	}

	if pos < 0 {
		return 0
	}

	return pos
}
