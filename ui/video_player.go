//go:build gui

package ui

import (
	"image"
	"sync"
	"time"
)

// videoPlayer shows a clip in a window, one frame at a time.
//
// It is the controller an FFmpeg decoder would also feed: the player knows
// frames and delays, not formats. It runs one goroutine per play, and every
// play, pause, stop or seek retires the previous one with a generation counter,
// so a wait that was already under way cannot draw a frame from a clip nobody is
// watching any more (#421).
type videoPlayer struct {
	mu sync.Mutex

	clip    *videoClip
	frame   int
	gen     int
	playing bool

	// origin is the clip position at which frame started, and started is when
	// that was: Position is the sum, capped at the duration, the same way the
	// audio player's clock works.
	origin  time.Duration
	started time.Time
	paused  time.Duration

	// wake is closed to interrupt the wait a retired run is sleeping in.
	wake func()

	onFrame func(image.Image)
	onDone  func()
}

// Play starts the clip at from, reporting every frame to onFrame and the end to
// onDone.
//
// onFrame runs on the player's goroutine: a caller with widgets wraps it in
// fyne.Do.
func (p *videoPlayer) Play(clip *videoClip, from time.Duration, onFrame func(image.Image), onDone func()) {
	if clip == nil || len(clip.frames) == 0 {
		return
	}

	p.stopRun()

	p.mu.Lock()
	frame, origin := clip.frameAt(from)

	p.clip = clip
	p.frame = frame
	p.origin = origin
	p.started = time.Now()
	p.playing = true
	p.paused = 0
	p.gen++
	gen := p.gen
	p.onFrame = onFrame
	p.onDone = onDone
	wake := make(chan struct{})
	p.wake = func() { close(wake) }
	p.mu.Unlock()

	go p.run(gen, wake)
}

// run shows frames until the clip ends or this run is retired.
func (p *videoPlayer) run(gen int, wake chan struct{}) {
	for {
		p.mu.Lock()
		if p.gen != gen || !p.playing {
			p.mu.Unlock()

			return
		}

		frame := p.frame
		delay := p.clip.delays[frame]
		onFrame := p.onFrame
		clip := p.clip
		p.mu.Unlock()

		if onFrame != nil {
			onFrame(clip.frames[frame])
		}

		timer := time.NewTimer(delay)

		select {
		case <-timer.C:
		case <-wake:
			timer.Stop()

			return
		}

		p.mu.Lock()
		if p.gen != gen || !p.playing {
			p.mu.Unlock()

			return
		}

		p.origin = clip.startOf(frame + 1)
		p.started = time.Now()
		p.frame = frame + 1

		if p.frame >= len(clip.frames) {
			p.playing = false
			p.frame = 0
			p.paused = 0
			onDone := p.onDone
			p.gen++
			p.mu.Unlock()

			if onDone != nil {
				onDone()
			}

			return
		}

		p.mu.Unlock()
	}
}

// Pause stops on the frame that is showing, and remembers where that is.
func (p *videoPlayer) Pause() {
	position := p.Position()
	p.stopRun()

	p.mu.Lock()
	if p.playing {
		p.paused = position
	}

	p.playing = false
	p.mu.Unlock()
}

// Stop silences the clip and forgets the position: the next Play starts over.
func (p *videoPlayer) Stop() {
	p.stopRun()

	p.mu.Lock()
	p.playing = false
	p.paused = 0
	p.frame = 0
	p.origin = 0
	p.mu.Unlock()
}

// Seek moves the playhead, continuing to play from there if it was playing.
func (p *videoPlayer) Seek(position time.Duration) {
	p.mu.Lock()
	clip := p.clip
	playing := p.playing
	onFrame, onDone := p.onFrame, p.onDone
	p.mu.Unlock()

	if clip == nil {
		return
	}

	p.stopRun()

	if playing {
		p.Play(clip, position, onFrame, onDone)

		return
	}

	p.mu.Lock()
	p.frame, p.origin = clip.frameAt(position)
	p.paused = position
	p.mu.Unlock()
}

// Position is how far into the clip the picture has got.
func (p *videoPlayer) Position() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.playing {
		return p.paused
	}

	position := p.origin + time.Since(p.started)

	if p.clip != nil && position > p.clip.duration {
		position = p.clip.duration
	}

	return position
}

// Playing reports whether the picture is moving.
func (p *videoPlayer) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.playing
}

// stopRun retires the goroutine that is running, if there is one.
func (p *videoPlayer) stopRun() {
	p.mu.Lock()
	p.gen++
	wake := p.wake
	p.wake = nil
	p.mu.Unlock()

	if wake != nil {
		wake()
	}
}

// frameAt is the frame a clip position falls in, and when that frame starts.
func (c *videoClip) frameAt(position time.Duration) (int, time.Duration) {
	if position <= 0 {
		return 0, 0
	}

	var start time.Duration

	for i, delay := range c.delays {
		if position < start+delay {
			return i, start
		}

		start += delay
	}

	// Past the end: the last frame, at its start.
	last := len(c.delays) - 1

	return last, c.startOf(last)
}

// startOf is when frame i begins in the clip.
func (c *videoClip) startOf(i int) time.Duration {
	var start time.Duration

	for j := 0; j < i && j < len(c.delays); j++ {
		start += c.delays[j]
	}

	return start
}
