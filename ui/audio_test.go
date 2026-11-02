//go:build gui

package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The fixtures are built here rather than committed: a WAV is a header and some
// samples, and a silent MP3 frame is a header and padding, so the test can state
// the shape it is checking instead of hiding it in a binary (#306).

// testWAV builds an uncompressed WAV with frames frames of silence.
func testWAV(t *testing.T, rate, channels, bits, frames int) []byte {
	t.Helper()

	bytesPerSample := bits / 8
	data := make([]byte, frames*channels*bytesPerSample)

	return wavAround(t, rate, channels, bits, data, 1)
}

// wavAround wraps data in a WAV container with the given format number.
func wavAround(t *testing.T, rate, channels, bits int, data []byte, format uint16) []byte {
	t.Helper()

	bytesPerSample := bits / 8

	var buf bytes.Buffer

	buf.WriteString("RIFF")

	if err := binary.Write(&buf, binary.LittleEndian, uint32(36+len(data))); err != nil {
		t.Fatal(err)
	}

	buf.WriteString("WAVE")
	buf.WriteString("fmt ")

	for _, value := range []any{
		uint32(16),
		format,
		uint16(channels),
		uint32(rate),
		uint32(rate * channels * bytesPerSample),
		uint16(channels * bytesPerSample),
		uint16(bits),
	} {
		if err := binary.Write(&buf, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}

	buf.WriteString("data")

	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(data))); err != nil {
		t.Fatal(err)
	}

	buf.Write(data)

	return buf.Bytes()
}

// testMP3 builds frames MPEG-1 Layer III frames of silence: eleven set bits,
// then a 128 kbit/s, 44.1 kHz, stereo frame of fixed length.
func testMP3(frames int) []byte {
	out := make([]byte, 0, frames*417)

	for i := 0; i < frames; i++ {
		frame := make([]byte, 417)
		frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00

		out = append(out, frame...)
	}

	return out
}

// writeTestAudio puts bytes in a temporary file and returns its path.
func writeTestAudio(t *testing.T, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// The decoder is chosen from the bytes, not the name: a phone's picker names a
// file with a document id and no extension (#303).
func TestAudioKindReadsTheHeader(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		want string
	}{
		{"wav", testWAV(t, 8000, 1, 16, 1), "wav"},
		{"mp3 with an ID3 tag", append([]byte("ID3\x04\x00\x00"), make([]byte, 8)...), "mp3"},
		{"bare mpeg frame", testMP3(1), "mp3"},
		{"ogg", []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00"), ""},
		{"m4a", []byte("\x00\x00\x00\x20ftypM4A \x00\x00\x00\x00"), ""},
		{"text", []byte("hello there\n"), ""},
		{"empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioKind(tt.head); got != tt.want {
				t.Errorf("audioKind(% q) = %q, want %q", tt.head[:min(len(tt.head), 12)], got, tt.want)
			}
		})
	}
}

// A mono clip at a rate the device does not use is duplicated to both channels
// and resampled: the window has one output format, so every clip is brought to
// it before it is played.
func TestDecodeWAVResamplesMonoToTheDeviceFormat(t *testing.T) {
	path := writeTestAudio(t, "memo.wav", testWAV(t, 8000, 1, 16, 8000))

	clip, err := decodeAudio(path)
	if err != nil {
		t.Fatalf("decodeAudio() error = %v", err)
	}

	// A second of 8 kHz mono becomes a second of 48 kHz stereo: 48000 frames of
	// two channels of two bytes.
	if want := audioSampleRate * audioChannels * 2; len(clip.pcm) != want {
		t.Errorf("pcm = %d bytes, want %d", len(clip.pcm), want)
	}

	if got, want := clip.duration, time.Second; got < want-time.Millisecond || got > want+time.Millisecond {
		t.Errorf("duration = %v, want about %v", got, want)
	}
}

// A clip already at the device's format is passed through without shrinking or
// padding it.
func TestDecodeWAVKeepsTheDeviceFormat(t *testing.T) {
	path := writeTestAudio(t, "clip.wav", testWAV(t, audioSampleRate, audioChannels, 16, audioSampleRate/2))

	clip, err := decodeAudio(path)
	if err != nil {
		t.Fatalf("decodeAudio() error = %v", err)
	}

	if want := audioSampleRate * audioChannels * 2 / 2; len(clip.pcm) != want {
		t.Errorf("pcm = %d bytes, want %d", len(clip.pcm), want)
	}

	if got, want := clip.duration, 500*time.Millisecond; got < want-time.Millisecond || got > want+time.Millisecond {
		t.Errorf("duration = %v, want about %v", got, want)
	}
}

// Eight-bit WAV is unsigned and centred on 128; the device wants signed
// samples, so the decode widens it.
func TestDecodeWAVWidensEightBitSamples(t *testing.T) {
	// 0x00 is the bottom of the range, 0x80 is the middle, 0xFF the top.
	data := []byte{0x00, 0x80, 0xFF}
	path := writeTestAudio(t, "eight.wav", wavAround(t, audioSampleRate, 1, 8, data, 1))

	clip, err := decodeAudio(path)
	if err != nil {
		t.Fatalf("decodeAudio() error = %v", err)
	}

	// The middle of the range is silence, and the two ends are opposite poles.
	left := int16(binary.LittleEndian.Uint16(clip.pcm[0:]))
	middle := int16(binary.LittleEndian.Uint16(clip.pcm[4:]))

	if left >= 0 {
		t.Errorf("the bottom of an eight-bit range decoded to %d, want a negative sample", left)
	}

	if middle != 0 {
		t.Errorf("the middle of an eight-bit range decoded to %d, want silence", middle)
	}
}

// A WAV whose format number is not PCM needs a codec this build does not have.
func TestDecodeWAVRefusesACompressedFormat(t *testing.T) {
	path := writeTestAudio(t, "float.wav", wavAround(t, audioSampleRate, 1, 16, make([]byte, 16), 3))

	if _, err := decodeAudio(path); !errors.Is(err, errAudioUnsupported) {
		t.Errorf("decodeAudio() error = %v, want %v", err, errAudioUnsupported)
	}
}

// A chunk length is the file's word, not ours: a broken or hostile value must
// not become an allocation.
func TestDecodeWAVRefusesAnImpossibleChunk(t *testing.T) {
	body := testWAV(t, audioSampleRate, 1, 16, 16)
	// The data chunk's length field, 40 bytes in: 4 for "data", then the size.
	binary.LittleEndian.PutUint32(body[40:], 0x7FFFFFFF)
	path := writeTestAudio(t, "huge.wav", body)

	if _, err := decodeAudio(path); err == nil {
		t.Fatal("decodeAudio() accepted a chunk larger than the file")
	}
}

// An MP3 decodes through the pure-Go decoder, at its own rate, and comes out in
// the device's format.
func TestDecodeMP3(t *testing.T) {
	path := writeTestAudio(t, "memo.mp3", testMP3(4))

	clip, err := decodeAudio(path)
	if err != nil {
		t.Fatalf("decodeAudio() error = %v", err)
	}

	if len(clip.pcm) == 0 {
		t.Fatal("the MP3 decoded to no samples")
	}

	if len(clip.pcm)%(audioChannels*2) != 0 {
		t.Errorf("pcm = %d bytes, want whole stereo frames", len(clip.pcm))
	}

	if clip.duration <= 0 {
		t.Errorf("duration = %v, want a positive length", clip.duration)
	}
}

// A format this build has no decoder for is a fact about the file, not a
// failure: the row says so and offers its other actions.
func TestDecodeAudioRefusesAFormatWithNoDecoder(t *testing.T) {
	path := writeTestAudio(t, "voice.ogg", append([]byte("OggS"), make([]byte, 64)...))

	if _, err := decodeAudio(path); !errors.Is(err, errAudioUnsupported) {
		t.Errorf("decodeAudio() error = %v, want %v", err, errAudioUnsupported)
	}
}

// canPlayAudio reads the header only, so a row can decide without decoding a
// clip it may never play.
func TestCanPlayAudioReadsTheHeader(t *testing.T) {
	saved := writeTestAudio(t, "memo.wav", testWAV(t, 8000, 1, 16, 10))
	if !canPlayAudio(saved) {
		t.Error("canPlayAudio() said no to a WAV")
	}

	other := writeTestAudio(t, "notes.pdf", []byte("%PDF-1.4"))
	if canPlayAudio(other) {
		t.Error("canPlayAudio() said yes to a PDF")
	}

	if canPlayAudio(filepath.Join(t.TempDir(), "missing.wav")) {
		t.Error("canPlayAudio() said yes to a file that is not there")
	}
}

// A clock the way a player shows one: minutes and seconds, and hours only when
// there are any.
func TestAudioClock(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "0:00"},
		{0, "0:00"},
		{7 * time.Second, "0:07"},
		{62 * time.Second, "1:02"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
		{1500 * time.Millisecond, "0:01"},
	}

	for _, tt := range tests {
		if got := audioClock(tt.in); got != tt.want {
			t.Errorf("audioClock(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The row's clock shows the length when the clip is idle, the position over the
// length while it sounds, and nothing when this window has never decoded it.
func TestAudioLabel(t *testing.T) {
	tests := []struct {
		name     string
		position time.Duration
		duration time.Duration
		want     string
	}{
		{"never decoded", 0, 0, ""},
		{"idle", 0, 12 * time.Second, "0:12"},
		{"playing", 3 * time.Second, 12 * time.Second, "0:03 / 0:12"},
		{"finished", 12 * time.Second, 12 * time.Second, "0:12 / 0:12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioLabel(tt.position, tt.duration); got != tt.want {
				t.Errorf("audioLabel(%v, %v) = %q, want %q", tt.position, tt.duration, got, tt.want)
			}
		})
	}
}
