//go:build gui

package ui

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// The player works in one format: a clip of any rate and channel count is
// converted to this one, so a voice memo at 8 kHz and a song at 44.1 kHz share
// the single device instead of asking the toolkit for two contexts (#306).
const (
	audioSampleRate = 48000
	audioChannels   = 2
)

// audioMaxPCM bounds a decoded clip. A voice memo is seconds long; a decoded
// audiobook is not something a transcript row should hold in memory, so a clip
// past this bound is refused with a sentence and stays an ordinary file row.
// 32 MiB is a little under three minutes at 48 kHz stereo.
const audioMaxPCM = 32 << 20

// errAudioUnsupported is a file this build has no decoder for, and
// errAudioTooLong a clip past the memory bound. Both are facts about the file
// or the build, not failures: the row says so and offers its other actions.
var (
	errAudioUnsupported = errors.New("no player for that format in this build")
	errAudioTooLong     = errors.New("the clip is too long to play inside the app")
)

// audioClip is a decoded sound, ready for the device: 48 kHz, stereo, signed
// 16-bit little-endian, and how long it lasts.
type audioClip struct {
	pcm      []byte
	duration time.Duration
}

// audioEngine is the window's sound device. There is one for the window, so one
// clip sounds at a time: Play replaces whatever was playing, and onDone reports
// the end — natural, stopped, or the reason it failed.
type audioEngine interface {
	Play(clip *audioClip, onDone func(error)) error
	Stop()
	Position() time.Duration

	// Seek moves the playhead, so Position reports from there (#399).
	Seek(position time.Duration) error
}

// audioKind names the decoder that can read the first bytes of a file, or ""
// when this build has none. The type is read from the bytes rather than the
// name: a phone's picker hands over a document id for a name (#303), and a
// suffix says what someone called a file, not what it holds.
func audioKind(head []byte) string {
	switch {
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return "wav"

	case len(head) >= 3 && string(head[0:3]) == "ID3":
		return "mp3"

	// A bare MPEG audio frame: eleven set bits, then the layer and version.
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "mp3"
	}

	return ""
}

// canPlayAudio reports whether this build can decode the file at path. It reads
// the header only, so a row can decide without decoding a clip it may never
// play.
func canPlayAudio(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	head := make([]byte, 12)

	n, _ := io.ReadFull(file, head)

	return audioKind(head[:n]) != ""
}

// decodeAudio reads a WAV or MP3 file into the engine's format.
//
// The whole clip is decoded before it is played, so the row can show its
// duration from the first second and the decode can happen off the UI
// goroutine. A file past audioMaxPCM is refused rather than streamed.
func decodeAudio(path string) (*audioClip, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	head := make([]byte, 12)

	n, _ := io.ReadFull(file, head)

	kind := audioKind(head[:n])
	if kind == "" {
		return nil, errAudioUnsupported
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	switch kind {
	case "wav":
		samples, rate, channels, err := readWAV(file)
		if err != nil {
			return nil, err
		}

		return clipFrom(samples, rate, channels), nil

	default:
		return decodeMP3(file)
	}
}

// readWAV reads an uncompressed (PCM) WAV file. A compressed WAV is refused:
// the format number says which codec it needs, and this build has none of them.
func readWAV(r io.Reader) ([]int16, int, int, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, 0, 0, err
	}

	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return nil, 0, 0, errAudioUnsupported
	}

	var (
		rate     int
		channels int
		bits     int
		format   uint16
	)

	for {
		chunk := make([]byte, 8)
		if _, err := io.ReadFull(r, chunk); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, 0, 0, errors.New("the WAV file ended before its audio data")
			}

			return nil, 0, 0, err
		}

		id := string(chunk[0:4])
		size := int64(binary.LittleEndian.Uint32(chunk[4:8]))

		// A chunk length is the file's word, not ours: a hostile or broken
		// value must not become an allocation.
		if size > int64(audioMaxPCM)*2 {
			return nil, 0, 0, errors.New("the WAV file declares an impossible chunk")
		}

		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, 0, errAudioUnsupported
			}

			// Only the first sixteen bytes are format fields this reader
			// understands; the rest of a longer `fmt ` chunk (extensible WAV
			// writes more) is skipped rather than allocated, because its length
			// is the file's word.
			buf := make([]byte, 16)
			if _, err := io.ReadFull(r, buf); err != nil {
				return nil, 0, 0, err
			}

			if rest := size - 16; rest > 0 {
				if _, err := io.CopyN(io.Discard, r, rest); err != nil {
					return nil, 0, 0, err
				}
			}

			format = binary.LittleEndian.Uint16(buf[0:2])
			channels = int(binary.LittleEndian.Uint16(buf[2:4]))
			rate = int(binary.LittleEndian.Uint32(buf[4:8]))
			bits = int(binary.LittleEndian.Uint16(buf[14:16]))

			if format != 1 {
				return nil, 0, 0, errAudioUnsupported
			}

			if channels != 1 && channels != 2 {
				return nil, 0, 0, errAudioUnsupported
			}

			if bits != 8 && bits != 16 {
				return nil, 0, 0, errAudioUnsupported
			}

			if rate <= 0 {
				return nil, 0, 0, errAudioUnsupported
			}

		case "data":
			if channels == 0 || rate == 0 {
				return nil, 0, 0, errors.New("the WAV file has no format before its audio data")
			}

			limit := int64(audioMaxPCM)
			if bits == 8 {
				// Eight-bit samples double when they are widened below.
				limit /= 2
			}

			if size > limit {
				return nil, 0, 0, errAudioTooLong
			}

			buf := make([]byte, size)
			if _, err := io.ReadFull(r, buf); err != nil {
				return nil, 0, 0, err
			}

			return widenWAV(buf, bits), rate, channels, nil

		default:
			if size%2 == 1 {
				size++
			}

			if _, err := io.CopyN(io.Discard, r, size); err != nil {
				return nil, 0, 0, err
			}
		}
	}
}

// widenWAV turns a data chunk into signed samples. Eight-bit WAV is unsigned
// and centred on 128; sixteen-bit is already signed little-endian.
func widenWAV(buf []byte, bits int) []int16 {
	if bits == 16 {
		samples := make([]int16, len(buf)/2)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
		}

		return samples
	}

	samples := make([]int16, len(buf))
	for i, b := range buf {
		samples[i] = int16(int(b)-128) << 8
	}

	return samples
}

// decodeMP3 reads the frames of an MP3 file. The decoder is pure Go, so it
// works in every GUI build this repository produces.
func decodeMP3(r io.Reader) (*audioClip, error) {
	decoder, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, err
	}

	rate := decoder.SampleRate()
	if rate <= 0 {
		return nil, errAudioUnsupported
	}

	pcm, err := io.ReadAll(io.LimitReader(decoder, audioMaxPCM+1))
	if err != nil {
		return nil, err
	}

	if len(pcm) > audioMaxPCM {
		return nil, errAudioTooLong
	}

	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}

	// An MP3 decoder always produces stereo.
	return clipFrom(samples, rate, 2), nil
}

// clipFrom converts interleaved samples to the engine's format and measures the
// result from the frames actually produced.
func clipFrom(samples []int16, rate, channels int) *audioClip {
	pcm := toOutput(samples, rate, channels)
	frames := len(pcm) / (audioChannels * 2)

	return &audioClip{
		pcm:      pcm,
		duration: time.Duration(int64(frames) * int64(time.Second) / audioSampleRate),
	}
}

// toOutput mixes and resamples interleaved 16-bit samples into 48 kHz stereo.
//
// A mono clip becomes both channels, and a rate change is a linear
// interpolation: a chat window needs a voice to be intelligible, not a
// mastering chain.
func toOutput(samples []int16, rate, channels int) []byte {
	if rate <= 0 || (channels != 1 && channels != 2) {
		return nil
	}

	frames := len(samples) / channels
	if frames == 0 {
		return nil
	}

	outFrames := int(int64(frames) * int64(audioSampleRate) / int64(rate))
	if outFrames < 1 {
		outFrames = 1
	}

	out := make([]byte, outFrames*audioChannels*2)
	step := float64(rate) / float64(audioSampleRate)

	for i := 0; i < outFrames; i++ {
		pos := float64(i) * step

		first := int(pos)
		if first >= frames {
			first = frames - 1
		}

		second := first + 1
		if second >= frames {
			second = frames - 1
		}

		frac := pos - float64(first)

		at := func(channel int) int16 {
			a := float64(samples[first*channels+channel])
			b := float64(samples[second*channels+channel])

			return int16(a + (b-a)*frac)
		}

		left := at(0)
		right := left
		if channels == 2 {
			right = at(1)
		}

		binary.LittleEndian.PutUint16(out[i*4:], uint16(left))
		binary.LittleEndian.PutUint16(out[i*4+2:], uint16(right))
	}

	return out
}

// audioPeakColumns is how many columns a waveform is reduced to. A row is a few
// hundred pixels wide at most, and the control resamples this to whatever it is
// given, so more columns would only cost memory.
const audioPeakColumns = 240

// audioPeaks reduces decoded PCM to one peak per column: the loudest sample of
// the slice of the clip that column stands for. A clip with no samples has no
// shape, and says so with nil (#399).
func audioPeaks(pcm []byte, columns int) []float32 {
	frame := audioChannels * 2
	if columns <= 0 || len(pcm) < frame {
		return nil
	}

	frames := len(pcm) / frame
	peaks := make([]float32, columns)

	for column := 0; column < columns; column++ {
		first := column * frames / columns
		last := (column + 1) * frames / columns

		if last <= first {
			last = first + 1
		}

		if last > frames {
			last = frames
		}

		var peak int32

		for f := first; f < last; f++ {
			for channel := 0; channel < audioChannels; channel++ {
				sample := int32(int16(binary.LittleEndian.Uint16(pcm[f*frame+channel*2:])))
				if sample < 0 {
					sample = -sample
				}

				if sample > peak {
					peak = sample
				}
			}
		}

		peaks[column] = float32(peak) / 32768
	}

	return peaks
}

// audioClock is a position or a duration the way a player shows it: 0:07,
// 1:02, 1:02:03. A negative value is the start, not a negative time.
func audioClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	total := int64(d / time.Second)
	hours := total / 3600
	minutes := (total / 60) % 60
	seconds := total % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}

	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
