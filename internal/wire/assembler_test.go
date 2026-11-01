package wire

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// transferFrames is what a sender puts on the wire for one file: every chunk,
// then the file-end that closes it.
func transferFrames(id, name, mime string, data []byte) []Frame {
	size := int64(len(data))

	frames := make([]Frame, 0, Chunks(size)+1)
	for i := 0; i < Chunks(size); i++ {
		h := chunkHeader(id, name, size, i)
		h.MIME = mime
		off := ChunkOffset(i)
		frames = append(frames, Frame{
			Header:  h,
			Payload: data[off : off+int64(ChunkLen(size, i))],
		})
	}

	end := endHeaderFor(id, name, data)
	end.MIME = mime
	frames = append(frames, Frame{Header: end})

	return frames
}

// feed adds every frame and returns the file the last one produced.
func feed(t *testing.T, a *Assembler, frames []Frame) *File {
	t.Helper()

	var done *File

	for i, f := range frames {
		file, err := a.Add(f)
		if err != nil {
			t.Fatalf("Add(frame %d) error = %v", i, err)
		}

		if i < len(frames)-1 && file != nil {
			t.Fatalf("frame %d finished a file before the last frame", i)
		}

		done = file
	}

	if done == nil {
		t.Fatal("the last frame produced no file")
	}

	return done
}

// entries lists a directory, so a test can say what the assembler left behind.
func entries(t *testing.T, dir string) []string {
	t.Helper()

	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name()
	}

	return names
}

// TestAssemblerReassemblesAFile walks the whole transfer: several chunks, then
// the file-end, with the bytes checked against what the sender had.
func TestAssemblerReassemblesAFile(t *testing.T) {
	data := bytes.Repeat([]byte{0x5A}, 2*ChunkSize+7)
	dir := t.TempDir()

	done := feed(t, NewAssembler(dir, time.Minute), transferFrames("id-1", "movie.mp4", "video/mp4", data))

	if done.Name != "movie.mp4" || done.Size != int64(len(data)) || done.MIME != "video/mp4" {
		t.Errorf("file = %+v, want movie.mp4, %d bytes, video/mp4", done, len(data))
	}

	if filepath.Dir(done.Path) != dir {
		t.Errorf("path = %s, want a file inside %s", done.Path, dir)
	}

	content, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatalf("reading the finished file: %v", err)
	}

	if !bytes.Equal(content, data) {
		t.Errorf("finished file is %d bytes, want %d identical", len(content), len(data))
	}

	if names := entries(t, dir); len(names) != 1 || names[0] != "movie.mp4" {
		t.Errorf("folder holds %v, want just the finished file", names)
	}
}

// TestAssemblerAcceptsASecondEnd checks that a finished transfer is gone: a
// repeated file-end must not produce a second file.
func TestAssemblerAcceptsASecondEnd(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	frames := transferFrames("id-1", "once.txt", "", []byte("once"))

	feed(t, a, frames)

	if _, err := a.Add(frames[len(frames)-1]); !errors.Is(err, ErrSequence) {
		t.Errorf("second file-end error = %v, want %v", err, ErrSequence)
	}
}

// TestAssemblerWritesAZeroByteFile covers the transfer with no chunks at all:
// the file-end is the whole thing, and its hash is the empty hash.
func TestAssemblerWritesAZeroByteFile(t *testing.T) {
	dir := t.TempDir()

	done := feed(t, NewAssembler(dir, time.Minute), transferFrames("id-1", "empty.txt", "text/plain", nil))

	if done.Size != 0 {
		t.Errorf("size = %d, want 0", done.Size)
	}

	content, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatalf("reading the empty file: %v", err)
	}

	if len(content) != 0 {
		t.Errorf("file holds %d bytes, want none", len(content))
	}

	if names := entries(t, dir); len(names) != 1 || names[0] != "empty.txt" {
		t.Errorf("folder holds %v, want just the empty file", names)
	}
}

// TestAssemblerRejectsAWrongHash is the check the whole package exists for.
func TestAssemblerRejectsAWrongHash(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	frames := transferFrames("id-1", "photo.jpg", "image/jpeg", bytes.Repeat([]byte{7}, ChunkSize+3))
	frames[len(frames)-1].Header.SHA256 = strings.Repeat("0", 64)

	for i, f := range frames {
		done, err := a.Add(f)
		if i < len(frames)-1 {
			if err != nil {
				t.Fatalf("chunk %d rejected: %v", i, err)
			}

			continue
		}

		if !errors.Is(err, ErrIntegrity) {
			t.Errorf("file-end error = %v, want %v", err, ErrIntegrity)
		}

		if done != nil {
			t.Errorf("a wrong hash produced %+v", done)
		}
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestAssemblerRejectsAShortFile is a stream that ended before every chunk
// arrived, which must not leave a truncated document.
func TestAssemblerRejectsAShortFile(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	data := bytes.Repeat([]byte{9}, 2*ChunkSize+7)
	frames := transferFrames("id-1", "notes.txt", "", data)

	if _, err := a.Add(frames[0]); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	if _, err := a.Add(frames[1]); err != nil {
		t.Fatalf("second chunk: %v", err)
	}

	if _, err := a.Add(frames[len(frames)-1]); !errors.Is(err, ErrIntegrity) {
		t.Errorf("file-end error = %v, want %v", err, ErrIntegrity)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestAssemblerRejectsOutOfOrder covers the three ways a stream can be wrong
// about order: starting late, skipping, and repeating.
func TestAssemblerRejectsOutOfOrder(t *testing.T) {
	data := bytes.Repeat([]byte{3}, 2*ChunkSize+7)
	frames := transferFrames("id-1", "movie.mp4", "", data)

	t.Run("starts at the second chunk", func(t *testing.T) {
		dir := t.TempDir()

		if _, err := NewAssembler(dir, time.Minute).Add(frames[1]); !errors.Is(err, ErrSequence) {
			t.Errorf("error = %v, want %v", err, ErrSequence)
		}

		if names := entries(t, dir); len(names) != 0 {
			t.Errorf("folder holds %v, want nothing", names)
		}
	})

	t.Run("skips a chunk", func(t *testing.T) {
		dir := t.TempDir()
		a := NewAssembler(dir, time.Minute)

		if _, err := a.Add(frames[0]); err != nil {
			t.Fatalf("first chunk: %v", err)
		}

		if _, err := a.Add(frames[2]); !errors.Is(err, ErrSequence) {
			t.Errorf("error = %v, want %v", err, ErrSequence)
		}

		if names := entries(t, dir); len(names) != 0 {
			t.Errorf("folder holds %v, want nothing", names)
		}
	})

	t.Run("repeats a chunk", func(t *testing.T) {
		dir := t.TempDir()
		a := NewAssembler(dir, time.Minute)

		if _, err := a.Add(frames[0]); err != nil {
			t.Fatalf("first chunk: %v", err)
		}

		if _, err := a.Add(frames[0]); !errors.Is(err, ErrSequence) {
			t.Errorf("error = %v, want %v", err, ErrSequence)
		}

		if names := entries(t, dir); len(names) != 0 {
			t.Errorf("folder holds %v, want nothing", names)
		}
	})
}

// TestAssemblerRejectsAChangedFile catches a stream that switches files halfway
// through: the second chunk describes a size the first one did not.
func TestAssemblerRejectsAChangedFile(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	data := bytes.Repeat([]byte{4}, 2*ChunkSize+7)
	frames := transferFrames("id-1", "movie.mp4", "", data)

	if _, err := a.Add(frames[0]); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	changed := frames[1]
	changed.Header.Size = int64(len(data)) - 1

	if _, err := a.Add(changed); !errors.Is(err, ErrHeader) {
		t.Errorf("error = %v, want %v", err, ErrHeader)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestAssemblerRejectsAChangedName catches a file-end that names a different
// file than the chunks that preceded it.
func TestAssemblerRejectsAChangedName(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	data := []byte("a small file")
	frames := transferFrames("id-1", "one.txt", "", data)

	for _, f := range frames[:len(frames)-1] {
		if _, err := a.Add(f); err != nil {
			t.Fatalf("chunk: %v", err)
		}
	}

	end := frames[len(frames)-1]
	end.Header.Name = "other.txt"

	if _, err := a.Add(end); !errors.Is(err, ErrHeader) {
		t.Errorf("error = %v, want %v", err, ErrHeader)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestAssemblerDiscardsAfterTimeout uses a clock the test controls: no
// sleeping, and the part file has to be gone.
func TestAssemblerDiscardsAfterTimeout(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAssembler(dir, 10*time.Second)
	a.now = func() time.Time { return now }

	frames := transferFrames("id-1", "movie.mp4", "", bytes.Repeat([]byte{5}, 2*ChunkSize+7))

	if _, err := a.Add(frames[0]); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	now = now.Add(11 * time.Second)

	if dropped := a.Sweep(); dropped != 1 {
		t.Errorf("Sweep() dropped %d, want 1", dropped)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}

	if _, err := a.Add(frames[1]); !errors.Is(err, ErrSequence) {
		t.Errorf("chunk after the timeout error = %v, want %v", err, ErrSequence)
	}
}

// TestAssemblerSweepsOnAdd is the same timeout without a ticker: the next frame
// of any transfer reaps the one that went quiet.
func TestAssemblerSweepsOnAdd(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAssembler(dir, 10*time.Second)
	a.now = func() time.Time { return now }

	stale := transferFrames("id-old", "old.bin", "", bytes.Repeat([]byte{1}, ChunkSize))
	fresh := transferFrames("id-new", "new.bin", "", bytes.Repeat([]byte{2}, ChunkSize))

	if _, err := a.Add(stale[0]); err != nil {
		t.Fatalf("starting the stale transfer: %v", err)
	}

	now = now.Add(11 * time.Second)

	if _, err := a.Add(fresh[0]); err != nil {
		t.Fatalf("starting the fresh transfer: %v", err)
	}

	if names := entries(t, dir); len(names) != 1 {
		t.Errorf("folder holds %v, want only the fresh part file", names)
	}

	// The stale transfer is gone: its own next chunk must fail.
	if _, err := a.Add(stale[1]); !errors.Is(err, ErrSequence) {
		t.Errorf("stale chunk error = %v, want %v", err, ErrSequence)
	}
}

// TestAssemblerCloseDiscards is what a session end does to a half-received
// file.
func TestAssemblerCloseDiscards(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)
	frames := transferFrames("id-1", "half.bin", "", bytes.Repeat([]byte{6}, ChunkSize+1))

	if _, err := a.Add(frames[0]); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	a.Close()

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}

	if _, err := a.Add(frames[1]); !errors.Is(err, ErrSequence) {
		t.Errorf("chunk after Close() error = %v, want %v", err, ErrSequence)
	}
}

// TestAssemblerNeverOverwrites keeps two files with the same name as two files.
func TestAssemblerNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)

	first := feed(t, a, transferFrames("id-1", "photo.jpg", "", []byte("first")))
	second := feed(t, a, transferFrames("id-2", "photo.jpg", "", []byte("second")))

	if first.Path == second.Path {
		t.Fatalf("both transfers landed on %s", first.Path)
	}

	for path, want := range map[string]string{first.Path: "first", second.Path: "second"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}

		if string(got) != want {
			t.Errorf("%s holds %q, want %q", path, got, want)
		}
	}

	if names := entries(t, dir); len(names) != 2 {
		t.Errorf("folder holds %v, want two files", names)
	}
}

// TestAssemblerRefusesAPath makes sure the one field a peer controls cannot
// point outside the download folder.
func TestAssemblerRefusesAPath(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)

	bad := Frame{Header: chunkHeader("id-1", "../escape", 1, 0), Payload: []byte("x")}

	if _, err := a.Add(bad); !errors.Is(err, ErrHeader) {
		t.Errorf("error = %v, want %v", err, ErrHeader)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}

	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file appeared outside the folder: %v", err)
	}
}

// TestAssemblerRefusesText is the caller mistake the sentinel exists for.
func TestAssemblerRefusesText(t *testing.T) {
	text, err := Parse([]byte("hello"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if _, err := NewAssembler(t.TempDir(), time.Minute).Add(text); !errors.Is(err, ErrNotTransfer) {
		t.Errorf("error = %v, want %v", err, ErrNotTransfer)
	}
}

// TestAssemblerRejectsBadFrames covers the checks that happen before anything
// is written, including a payload longer than its header allows.
func TestAssemblerRejectsBadFrames(t *testing.T) {
	tests := []struct {
		name string
		f    Frame
		want error
	}{
		{
			name: "payload longer than the chunk",
			f:    Frame{Header: chunkHeader("id-1", "x", 1, 0), Payload: []byte("ab")},
			want: ErrPayload,
		},
		{
			name: "negative size",
			f:    Frame{Header: Header{Kind: KindFile, ID: "id-1", Name: "x", Size: -1}},
			want: ErrHeader,
		},
		{
			name: "file-end without a hash",
			f:    Frame{Header: Header{Kind: KindFileEnd, ID: "id-1", Name: "x", Size: 1, Chunks: 1}},
			want: ErrHeader,
		},
		{
			name: "end of a file that never started",
			f:    Frame{Header: endHeaderFor("id-1", "x", []byte("unseen"))},
			want: ErrSequence,
		},
		{
			name: "unknown kind",
			f:    Frame{Header: Header{Kind: "blob"}},
			want: ErrHeader,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if _, err := NewAssembler(dir, time.Minute).Add(tt.f); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}

			if names := entries(t, dir); len(names) != 0 {
				t.Errorf("folder holds %v, want nothing", names)
			}
		})
	}
}

// TestAssemblerInterleavesTwoTransfers is the case a chat has: two files from
// two peers arriving at once must not be mixed into each other.
func TestAssemblerInterleavesTwoTransfers(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)

	one := []byte("the first file")
	two := []byte("and a second file")
	first := transferFrames("id-1", "one.txt", "", one)
	second := transferFrames("id-2", "two.txt", "", two)

	// Both are single-chunk files apart from the end, so alternate chunk,
	// chunk, end, end.
	var done []*File

	for _, f := range []Frame{first[0], second[0], first[1], second[1]} {
		file, err := a.Add(f)
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}

		if file != nil {
			done = append(done, file)
		}
	}

	if len(done) != 2 {
		t.Fatalf("finished %d files, want 2", len(done))
	}

	for i, want := range []string{"the first file", "and a second file"} {
		got, err := os.ReadFile(done[i].Path)
		if err != nil {
			t.Fatalf("reading %s: %v", done[i].Path, err)
		}

		if string(got) != want {
			t.Errorf("%s holds %q, want %q", done[i].Path, got, want)
		}
	}
}

// TestAssemblerRejectsAWrongHashForAnEmptyFile checks the one transfer with no
// bytes to compare: a zero-byte file still has to prove its hash.
func TestAssemblerRejectsAWrongHashForAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)

	end := endHeaderFor("id-1", "empty.txt", nil)
	end.SHA256 = strings.Repeat("0", 64)

	if _, err := a.Add(Frame{Header: end}); !errors.Is(err, ErrIntegrity) {
		t.Errorf("error = %v, want %v", err, ErrIntegrity)
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestAssemblerReportsAMissingFolder is the setup mistake: a receiver pointed
// at a directory that does not exist says so instead of writing somewhere else.
func TestAssemblerReportsAMissingFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-there")
	a := NewAssembler(dir, time.Minute)

	frames := transferFrames("id-1", "x.bin", "", []byte("x"))

	if _, err := a.Add(frames[0]); err == nil {
		t.Fatal("Add() succeeded with no folder to write into")
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Add() created %s; the caller owns the folder", dir)
	}
}

// TestAssemblerDiscardsOnWriteFailure closes the part file from under the
// assembler, which is the white-box way to make a write fail. The transfer must
// be dropped, so the next chunk is not appended to a stream that is already
// broken.
func TestAssemblerDiscardsOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	a := NewAssembler(dir, time.Minute)

	frames := transferFrames("id-1", "movie.mp4", "", bytes.Repeat([]byte{8}, ChunkSize+1))

	if _, err := a.Add(frames[0]); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	if err := a.transfers["id-1"].file.Close(); err != nil {
		t.Fatalf("closing the part file: %v", err)
	}

	if _, err := a.Add(frames[1]); err == nil {
		t.Fatal("Add() wrote to a closed part file")
	}

	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("folder holds %v, want nothing", names)
	}
}

// TestZeroValueAssembler checks that NewAssembler is a convenience, not a
// requirement: a struct with only Dir filled in still works.
func TestZeroValueAssembler(t *testing.T) {
	dir := t.TempDir()
	a := &Assembler{Dir: dir}

	data := []byte("typed into the struct")
	done := feed(t, a, transferFrames("id-1", "notes.txt", "", data))

	if got, err := os.ReadFile(done.Path); err != nil || !bytes.Equal(got, data) {
		t.Errorf("reading %s = %q, %v", done.Path, got, err)
	}
}

// Abandoning a transfer that is still arriving removes its part file and its
// memory of the bytes, so a cancelled receive leaves nothing behind.
func TestAssemblerAbandon(t *testing.T) {
	dir := t.TempDir()
	assembler := &Assembler{Dir: dir}

	const id = "cancel-me"

	size := int64(ChunkSize * 2)
	header := chunkHeader(id, "clip.mp3", size, 0)
	header.MIME = "audio/mpeg"

	if _, err := assembler.Add(Frame{Header: header, Payload: make([]byte, ChunkSize)}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if len(assembler.transfers) != 1 {
		t.Fatalf("the assembler holds %d transfers, want 1", len(assembler.transfers))
	}

	if !assembler.Abandon(id) {
		t.Fatal("Abandon() reported nothing to abandon")
	}

	if len(assembler.transfers) != 0 {
		t.Errorf("the assembler still holds %d transfers after Abandon", len(assembler.transfers))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("Abandon left %d file(s) behind: %v", len(entries), entries)
	}

	if assembler.Abandon(id) {
		t.Error("Abandon() reported a transfer the second time, want nothing left to abandon")
	}
}
