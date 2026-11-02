//go:build gui

package ui

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"os"
	"time"
)

// An animation this window will play, and the bounds it must respect.
//
// The bytes arrive from a peer, so every number in the file is theirs: the
// bounds are checked before anything is allocated, and a file past them is
// refused with a sentence rather than decoded until the window runs out of
// memory (#421).
const (
	// videoMaxBytes bounds the file on disk.
	videoMaxBytes = 16 << 20

	// videoMaxFrames bounds how many frames an animation may have. A chat
	// window is not a cinema: ten minutes at 20 fps is already more than this.
	videoMaxFrames = 1200

	// videoMaxPixels bounds the total decoded pixels across the frames, which
	// is what actually costs memory: one canvas per frame.
	videoMaxPixels = 64 << 20
)

// ErrVideoUnsupported is a format this build has no decoder for, or a picture
// that does not move; errVideoTooBig is an animation past the bounds above.
var (
	errVideoUnsupported = errors.New("no video decoder for that format in this build")
	errVideoTooBig      = errors.New("the animation is too large to play inside the app")
)

// videoClip is a decoded animation: the frames, how long each stays, and the
// total.
type videoClip struct {
	frames   []image.Image
	delays   []time.Duration
	duration time.Duration
}

// videoKind names the decoder that can read the first bytes of a file, or ""
// when this build has none.
//
// The type is read from the bytes rather than the name, for the reason audioKind
// does it: a phone's picker hands over a document id for a name (#303). Only
// formats decodable in pure Go are here; MP4, H.264 and WebM need a codec this
// build does not carry (#389).
func videoKind(head []byte) string {
	if len(head) >= 6 {
		switch string(head[0:6]) {
		case "GIF87a", "GIF89a":
			return "gif"
		}
	}

	return ""
}

// canPlayVideo reports whether this build can animate the file at path. It
// reads the header only, so a row can decide without decoding a picture it may
// never play.
func canPlayVideo(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	head := make([]byte, 6)

	n, _ := io.ReadFull(file, head)

	return videoKind(head[:n]) != ""
}

// decodeVideo reads an animation into frames.
func decodeVideo(path string) (*videoClip, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() > videoMaxBytes {
		return nil, errVideoTooBig
	}

	head := make([]byte, 6)

	n, _ := io.ReadFull(file, head)

	if videoKind(head[:n]) == "" {
		return nil, errVideoUnsupported
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(io.LimitReader(file, videoMaxBytes+1))
	if err != nil {
		return nil, err
	}

	if len(data) > videoMaxBytes {
		return nil, errVideoTooBig
	}

	return decodeGIF(data)
}

// decodeGIF turns a GIF into frames, compositing the partial frames a GIF is
// allowed to carry.
//
// A GIF frame is a rectangle that may cover part of the picture, and the
// disposal method of the frame before says what to do with what it drew:
// leave it, clear it to transparent, or restore what was there before. Drawing
// each frame onto a canvas is what makes the result look like the animation the
// sender saw.
func decodeGIF(data []byte) (*videoClip, error) {
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if len(animation.Image) < 2 {
		// A still picture is not a video, and drawing it as one would take the
		// row's picture away from the path that already handles it.
		return nil, errVideoUnsupported
	}

	if len(animation.Image) > videoMaxFrames {
		return nil, errVideoTooBig
	}

	width, height := animation.Config.Width, animation.Config.Height
	if width <= 0 || height <= 0 {
		return nil, errVideoUnsupported
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	var restore *image.RGBA

	clip := &videoClip{
		frames: make([]image.Image, 0, len(animation.Image)),
		delays: make([]time.Duration, 0, len(animation.Image)),
	}

	var pixels int64

	for i, frame := range animation.Image {
		if i > 0 {
			switch animation.Disposal[i-1] {
			case gif.DisposalBackground:
				draw.Draw(canvas, animation.Image[i-1].Bounds(), image.Transparent, image.Point{}, draw.Src)

			case gif.DisposalPrevious:
				if restore != nil {
					copy(canvas.Pix, restore.Pix)
				}
			}
		}

		if animation.Disposal[i] == gif.DisposalPrevious {
			restore = image.NewRGBA(canvas.Bounds())
			copy(restore.Pix, canvas.Pix)
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		pixels += int64(width) * int64(height)
		if pixels > videoMaxPixels {
			return nil, errVideoTooBig
		}

		snapshot := image.NewRGBA(canvas.Bounds())
		copy(snapshot.Pix, canvas.Pix)

		delay := gifFrameDelay(animation, i)

		clip.frames = append(clip.frames, snapshot)
		clip.delays = append(clip.delays, delay)
		clip.duration += delay
	}

	return clip, nil
}

// gifFrameDelay is how long one frame stays.
//
// A GIF stores hundredths of a second, and zero means "as fast as possible",
// which viewers have long read as a tenth of a second: honouring zero literally
// would make some animations invisible.
func gifFrameDelay(animation *gif.GIF, i int) time.Duration {
	hundredths := animation.Delay[i]
	if hundredths <= 0 {
		hundredths = 10
	}

	return time.Duration(hundredths) * 10 * time.Millisecond
}

// videoClock is the position and length the way a player shows them.
func videoLabel(position, duration time.Duration) string {
	if duration <= 0 {
		return ""
	}

	return audioClock(position) + " / " + audioClock(duration)
}
