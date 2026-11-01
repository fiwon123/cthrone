package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/wire"
)

// recordingSender stands in for a session: it keeps what the transport was
// handed, and can fail once told to.
type recordingSender struct {
	frames [][]byte
	failAt int // the 1-based Send that fails; 0 never fails
}

func (s *recordingSender) Send(payload []byte) error {
	s.frames = append(s.frames, append([]byte(nil), payload...))

	if s.failAt > 0 && len(s.frames) == s.failAt {
		return errors.New("peer went away")
	}

	return nil
}

// assembled feeds the recorded frames back into the receiver, which is the only
// check that matters: what a peer would do with them.
func (s *recordingSender) assembled(t *testing.T) []*wire.File {
	t.Helper()

	a := wire.NewAssembler(t.TempDir(), 0)
	var files []*wire.File

	for i, data := range s.frames {
		frame, err := wire.Parse(data)
		if err != nil {
			t.Fatalf("frame %d does not parse: %v", i, err)
		}

		file, err := a.Add(frame)
		if err != nil {
			t.Fatalf("frame %d is rejected: %v", i, err)
		}

		if file != nil {
			files = append(files, file)
		}
	}

	return files
}

// TestSendFileArrivesIntact drives a multi-chunk file through the sender and
// back through the receiver's assembler.
func TestSendFileArrivesIntact(t *testing.T) {
	data := bytes.Repeat([]byte("cthrone "), wire.ChunkSize/2+7)
	sender := &recordingSender{}

	var progress []int64

	err := sendFile(context.Background(), sender, "id-1", "notes.txt", "text/plain",
		int64(len(data)), bytes.NewReader(data), func(sent int64) {
			progress = append(progress, sent)
		})
	if err != nil {
		t.Fatalf("sendFile() error = %v", err)
	}

	wantFrames := wire.Chunks(int64(len(data))) + 1
	if len(sender.frames) != wantFrames {
		t.Errorf("sent %d frames, want %d", len(sender.frames), wantFrames)
	}

	files := sender.assembled(t)
	if len(files) != 1 {
		t.Fatalf("the receiver produced %d files, want 1", len(files))
	}

	got, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatalf("reading the received file: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("received %d bytes, want the %d that were sent", len(got), len(data))
	}

	if files[0].Name != "notes.txt" || files[0].MIME != "text/plain" {
		t.Errorf("file = %+v, want notes.txt as text/plain", files[0])
	}

	// Progress never goes backwards, ends at the full size, and has one report
	// per frame.
	if len(progress) != wantFrames {
		t.Fatalf("reported progress %d times, want %d", len(progress), wantFrames)
	}

	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress went backwards: %v", progress)
		}
	}

	if last := progress[len(progress)-1]; last != int64(len(data)) {
		t.Errorf("last progress = %d, want %d", last, len(data))
	}
}

// TestSendFileEmpty covers the file with no chunks: one frame, the end.
func TestSendFileEmpty(t *testing.T) {
	sender := &recordingSender{}
	var last int64

	err := sendFile(context.Background(), sender, "id-1", "empty.txt", "text/plain", 0,
		bytes.NewReader(nil), func(sent int64) { last = sent })
	if err != nil {
		t.Fatalf("sendFile() error = %v", err)
	}

	if len(sender.frames) != 1 {
		t.Errorf("sent %d frames, want just the end", len(sender.frames))
	}

	if last != 0 {
		t.Errorf("progress = %d, want 0", last)
	}

	files := sender.assembled(t)
	if len(files) != 1 || files[0].Size != 0 {
		t.Fatalf("receiver produced %+v, want one empty file", files)
	}
}

// TestSendFileStopsWhenTheReaderIsShort is a file that shrank after it was
// staged: the header promises more than the reader has, and the transfer must
// not close with a hash that describes a shorter file.
func TestSendFileStopsWhenTheReaderIsShort(t *testing.T) {
	sender := &recordingSender{}
	data := bytes.Repeat([]byte{1}, wire.ChunkSize+10)

	err := sendFile(context.Background(), sender, "id-1", "shrank.bin", "", int64(len(data)),
		bytes.NewReader(data[:10]), nil)
	if err == nil {
		t.Fatal("sendFile() accepted a reader shorter than the size")
	}

	if len(sender.frames) != 0 {
		t.Errorf("sent %d frames before noticing, want 0", len(sender.frames))
	}
}

// TestSendFileStopsWhenTheTransportFails covers a peer that goes away in the
// middle: the error reaches the caller, and no end frame claims success.
func TestSendFileStopsWhenTheTransportFails(t *testing.T) {
	sender := &recordingSender{failAt: 2}
	data := bytes.Repeat([]byte{2}, 2*wire.ChunkSize+1)

	err := sendFile(context.Background(), sender, "id-1", "movie.mp4", "video/mp4",
		int64(len(data)), bytes.NewReader(data), nil)
	if err == nil {
		t.Fatal("sendFile() accepted a failing transport")
	}

	if len(sender.frames) != 2 {
		t.Errorf("sent %d frames, want 2", len(sender.frames))
	}

	last, parseErr := wire.Parse(sender.frames[len(sender.frames)-1])
	if parseErr != nil {
		t.Fatalf("last frame does not parse: %v", parseErr)
	}

	if last.Header.Kind == wire.KindFileEnd {
		t.Error("a failed transfer still sent its end frame")
	}
}

// TestSendFileCancelsBetweenChunks is the Stop button during a transfer.
func TestSendFileCancelsBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sender := &recordingSender{}
	data := bytes.Repeat([]byte{3}, 3*wire.ChunkSize)

	// Cancel as soon as the first chunk is on its way.
	progress := func(int64) { cancel() }

	err := sendFile(ctx, sender, "id-1", "movie.mp4", "", int64(len(data)),
		bytes.NewReader(data), progress)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	if len(sender.frames) != 1 {
		t.Errorf("sent %d frames after cancelling, want 1", len(sender.frames))
	}
}

// TestStageCopiesAndMeasures is the reason staging exists: the picker hands over
// a stream with no length, and every header needs the length before the first
// chunk.
func TestStageCopiesAndMeasures(t *testing.T) {
	dir := t.TempDir()
	data := []byte("a picked file")

	path, size, err := stage(bytes.NewReader(data), dir)
	if err != nil {
		t.Fatalf("stage() error = %v", err)
	}

	if size != int64(len(data)) {
		t.Errorf("size = %d, want %d", size, len(data))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the staged copy: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("staged copy = %q, want %q", got, data)
	}

	if filepath.Dir(path) != dir {
		t.Errorf("staged copy is in %s, want %s", filepath.Dir(path), dir)
	}

	if err := os.Remove(path); err != nil {
		t.Errorf("removing the staged copy: %v", err)
	}
}

// TestStageLeavesNothingOnFailure keeps a failed copy from becoming litter.
func TestStageLeavesNothingOnFailure(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := stage(failingReader{}, dir); err == nil {
		t.Fatal("stage() accepted a failing reader")
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	if len(items) != 0 {
		t.Errorf("staging left %d files behind", len(items))
	}
}

// failingReader is a picker that breaks halfway, which is what a revoked
// permission looks like.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("permission revoked") }

// TestNewTransferID makes sure two transfers cannot share an id by accident.
func TestNewTransferID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newTransferID()
		if id == "" {
			t.Fatal("empty transfer id")
		}

		if seen[id] {
			t.Fatalf("duplicate transfer id %q", id)
		}

		seen[id] = true
	}
}

// TestPercent covers the row's arithmetic at the edges, where a division by
// zero or a 101% would look like a bug to the user.
func TestPercent(t *testing.T) {
	tests := []struct {
		sent, total int64
		want        int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{0, 0, 100},
		{-1, 100, 0},
		{200, 100, 100},
	}

	for _, tt := range tests {
		if got := percent(tt.sent, tt.total); got != tt.want {
			t.Errorf("percent(%d, %d) = %d, want %d", tt.sent, tt.total, got, tt.want)
		}
	}
}

// TestHumanBytes pins the sizes a row shows.
func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024 * 1024, "5.0 GB"},
	}

	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// TestShowsInline is the inline-or-row decision: a type the toolkit cannot
// decode is a row even when it says image/.
func TestShowsInline(t *testing.T) {
	tests := []struct {
		name string
		mime string
		size int64
		want bool
	}{
		{"png", "image/png", 1024, true},
		{"jpeg with parameters", "image/jpeg; charset=binary", 1024, true},
		{"svg", "image/svg+xml", 1024, true},
		{"uppercase", "IMAGE/PNG", 1024, true},
		{"a zip", "application/zip", 1024, false},
		{"an unknown image type", "image/webp", 1024, false},
		{"empty type", "", 1024, false},
		{"too big", "image/png", inlineImageMaxBytes + 1, false},
		{"empty", "image/png", 0, false},
	}

	for _, tt := range tests {
		if got := showsInline(tt.mime, tt.size); got != tt.want {
			t.Errorf("%s: showsInline(%q, %d) = %v, want %v", tt.name, tt.mime, tt.size, got, tt.want)
		}
	}
}

// TestResolveDownloadDir keeps downloads out of the directory that holds the
// secret, and gives a phone a folder it can write.
func TestResolveDownloadDir(t *testing.T) {
	if mobile := resolveDownloadDir(true, "/data/app"); mobile != filepath.Join("/data/app", "downloads") {
		t.Errorf("mobile download dir = %s", mobile)
	}

	desktop := resolveDownloadDir(false, "/data/app")
	if strings.Contains(desktop, "cthrone") && strings.Contains(desktop, filepath.Join(".config", "cthrone")) {
		t.Errorf("desktop download dir %s is the secret directory", desktop)
	}

	if !strings.HasSuffix(desktop, "Downloads") && !strings.Contains(desktop, "cthrone") {
		t.Errorf("desktop download dir = %s, want a downloads folder or the temporary fallback", desktop)
	}
}

// TestFileNoteLines pins what a transfer row says in each state.
func TestFileNoteLines(t *testing.T) {
	tests := []struct {
		name string
		note fileNote
		want string
	}{
		{
			name: "sending",
			note: fileNote{name: "photo.jpg", size: 200, sent: 50, state: transferSending},
			want: "photo.jpg — 25% of 200 B",
		},
		{
			name: "sent",
			note: fileNote{name: "photo.jpg", size: 1024, sent: 1024, state: transferSent},
			want: "photo.jpg (1.0 KB) — sent",
		},
		{
			name: "saved",
			note: fileNote{name: "notes.txt", size: 12, state: transferSaved, path: "/tmp/notes.txt"},
			want: "notes.txt (12 B) — saved to /tmp/notes.txt",
		},
		{
			name: "saved without a path",
			note: fileNote{name: "notes.txt", size: 12, state: transferSaved},
			want: "notes.txt (12 B) — saved",
		},
		{
			name: "waiting for the user",
			note: fileNote{name: "photo.jpg", size: 1024, state: transferPending},
			want: "photo.jpg (1.0 KB) — waiting to be saved",
		},
		{
			name: "failed",
			note: fileNote{name: "movie.mp4", size: 10, state: transferFailed},
			want: "movie.mp4 — failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.note.line(); got != tt.want {
				t.Errorf("line() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestChatMessageWithAFile checks the sender prefix is kept for a transfer row.
func TestChatMessageWithAFile(t *testing.T) {
	m := chatMessage{from: "phone", file: &fileNote{name: "a.png", size: 1024, state: transferSent}}
	if got, want := m.line(), "phone: a.png (1.0 KB) — sent"; got != want {
		t.Errorf("line() = %q, want %q", got, want)
	}
}

// TestSendFileHashMatchesTheWholeFile is the property the receiver checks: the
// end frame's hash is the file's, not the last chunk's.
func TestSendFileHashMatchesTheWholeFile(t *testing.T) {
	data := bytes.Repeat([]byte{0x7E}, 2*wire.ChunkSize+3)
	sender := &recordingSender{}

	if err := sendFile(context.Background(), sender, "id-1", "x.bin", "", int64(len(data)), bytes.NewReader(data), nil); err != nil {
		t.Fatalf("sendFile() error = %v", err)
	}

	end, err := wire.Parse(sender.frames[len(sender.frames)-1])
	if err != nil {
		t.Fatalf("parsing the end frame: %v", err)
	}

	if want := wire.SumHex(sha256.Sum256(data)); end.Header.SHA256 != want {
		t.Errorf("hash = %s, want %s", end.Header.SHA256, want)
	}
}

// TestSendFileReadsExactlyTheStagedFile refuses to send past the promised size:
// the extra bytes would be dropped by the framing anyway, so the sender would be
// reporting a success it did not have.
func TestSendFileReadsExactlyTheStagedFile(t *testing.T) {
	sender := &recordingSender{}
	extra := bytes.NewReader([]byte("more than promised"))

	if err := sendFile(context.Background(), sender, "id-1", "x.bin", "", 4, extra, nil); err != nil {
		t.Fatalf("sendFile() error = %v", err)
	}

	// Only the promised four bytes are read; the rest of the reader is left
	// alone rather than being silently sent.
	rest, _ := io.ReadAll(extra)
	if len(rest) == 0 {
		t.Error("sendFile() consumed past the size it promised")
	}
}

// TestDropFile is a cancelled transfer's row leaving the transcript: the row
// goes, the lines around it stay, and a note that is not in the log is not an
// error.
func TestDropFile(t *testing.T) {
	note := &fileNote{name: "movie.mp4", size: 10, state: transferSending}
	messages := []chatMessage{
		{from: "peer", text: "before"},
		{file: note},
		{text: "after"},
	}

	got, found := dropFile(messages, note)
	if !found {
		t.Fatal("dropFile() did not find the row")
	}

	if len(got) != 2 {
		t.Fatalf("left %d rows, want 2", len(got))
	}

	if got[0].line() != "peer: before" || got[1].line() != "You: after" {
		t.Errorf("rows = %q, %q, want the lines around the dropped row", got[0].line(), got[1].line())
	}

	if _, found := dropFile(got, note); found {
		t.Error("dropFile() found the row twice")
	}
}

// A text file is drawn in the row, and the drawing is bounded twice: by bytes
// read and by lines kept, so a pasted log ends in an ellipsis instead of
// pushing the conversation off the screen.
func TestTextPreview(t *testing.T) {
	t.Run("the beginning is shown", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "notes.txt")
		if err := os.WriteFile(path, []byte("first line\nsecond line\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		if got := textPreview(path); !strings.Contains(got, "first line") || !strings.Contains(got, "second line") {
			t.Errorf("textPreview() = %q, want both lines", got)
		}
	})

	t.Run("too many lines are cut", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.txt")

		var body strings.Builder
		for i := 0; i < inlineTextLines+5; i++ {
			fmt.Fprintf(&body, "line %d\n", i)
		}

		if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
			t.Fatal(err)
		}

		got := textPreview(path)

		if lines := strings.Count(got, "\n"); lines >= inlineTextLines {
			t.Errorf("textPreview() kept %d line breaks, want fewer than %d", lines, inlineTextLines)
		}

		if !strings.HasSuffix(got, "…") {
			t.Errorf("textPreview() = %q, want it to end in an ellipsis", got)
		}
	})

	t.Run("a file that cannot be read is nothing", func(t *testing.T) {
		if got := textPreview(filepath.Join(t.TempDir(), "absent.txt")); got != "" {
			t.Errorf("textPreview() = %q for a missing file, want empty", got)
		}
	})

	t.Run("an empty file is nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.txt")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		if got := textPreview(path); got != "" {
			t.Errorf("textPreview() = %q for an empty file, want empty", got)
		}
	})
}

// The type decides, and only types that are text are drawn: an image is the
// picture path's business, and a binary is a file row.
func TestShowsText(t *testing.T) {
	tests := []struct {
		name string
		mime string
		size int64
		want bool
	}{
		{"plain text", "text/plain", 100, true},
		{"text with parameters", "text/plain; charset=utf-8", 100, true},
		{"markdown", "text/markdown", 100, true},
		{"an empty file", "text/plain", 0, false},
		{"a picture", "image/png", 100, false},
		{"a binary", "application/octet-stream", 100, false},
		{"too large to draw", "text/plain", inlineTextBytes * 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := showsText(tt.mime, tt.size); got != tt.want {
				t.Errorf("showsText(%q, %d) = %v, want %v", tt.mime, tt.size, got, tt.want)
			}
		})
	}
}
