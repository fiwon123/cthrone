package ui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiwon123/cthrone/internal/wire"
)

const (
	// inlineImageMaxBytes bounds the picture drawn inside the transcript. A
	// phone decoding a 60-megapixel photo to draw it at 200dp is the freeze
	// this cap exists to prevent, so a larger image becomes an ordinary file
	// row with a Save action like any other.
	inlineImageMaxBytes = 8 << 20

	// inlineImageHeight is the height a picture occupies in the transcript.
	// The width follows the row, so the same code draws on a phone and on a
	// desktop window.
	inlineImageHeight = 180

	// inlineTextBytes bounds the text drawn inside the transcript, and
	// inlineTextLines bounds how many of its lines are drawn at all.
	//
	// A pasted log is a file like any other, and a row that grows to the length
	// of the log pushes the conversation off the screen. The reader gets the
	// beginning, an ellipsis, and Save as… for the rest.
	inlineTextBytes = 4 << 10
	inlineTextLines = 8
)

// inlineTypes are the image types the GUI can decode. A type outside this set
// becomes a file row even when its MIME says "image/": a type the toolkit
// cannot decode draws an empty box, which looks like a failed transfer.
// textPreviewPrefix is the MIME prefix whose files are drawn as text. The
// decision is made on the type the sender announced, and the file's own bytes
// decide whether it is readable at all.
const textPreviewPrefix = "text/"

var inlineTypes = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/gif":     true,
	"image/svg+xml": true,
}

// chunkSender is the part of a session a transfer needs.
//
// It is an interface because a session needs a socket and a test does not: the
// chunk order, the closing frame and the progress report are what can be wrong,
// and none of them needs a peer to check.
type chunkSender interface {
	Send(payload []byte) error
}

// sendFile publishes r as one wire transfer and returns when the end has been
// sent.
//
// The reader is the staged copy of the picked file, so size is its real length:
// the header carries it, the receiver checks it, and a reader shorter than size
// is an error rather than a file that arrives truncated on purpose. Cancelling
// ctx between chunks stops the transfer; the caller removes the staged file.
//
// onProgress is called with the number of bytes handed to the transport so far,
// including the final size, so a row can show 100% before the transfer is
// closed.
func sendFile(ctx context.Context, s chunkSender, id, name, mime string, size int64, r io.Reader, onProgress func(sent int64)) error {
	chunks := wire.Chunks(size)
	sum := sha256.New()

	for index := 0; index < chunks; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		length := wire.ChunkLen(size, index)
		buf := make([]byte, length)

		if _, err := io.ReadFull(r, buf); err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		sum.Write(buf)

		frame, err := wire.Encode(wire.Header{
			Kind:   wire.KindFile,
			ID:     id,
			Name:   name,
			MIME:   mime,
			Size:   size,
			Index:  index,
			Chunks: chunks,
		}, buf)
		if err != nil {
			return err
		}

		if err := s.Send(frame); err != nil {
			return fmt.Errorf("sending %s: %w", name, err)
		}

		if onProgress != nil {
			onProgress(wire.ChunkOffset(index) + int64(length))
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	end, err := wire.Encode(wire.Header{
		Kind:   wire.KindFileEnd,
		ID:     id,
		Name:   name,
		MIME:   mime,
		Size:   size,
		Chunks: chunks,
		SHA256: hex.EncodeToString(sum.Sum(nil)),
	}, nil)
	if err != nil {
		return err
	}

	if err := s.Send(end); err != nil {
		return fmt.Errorf("closing %s: %w", name, err)
	}

	if onProgress != nil {
		onProgress(size)
	}

	return nil
}

// stage copies a picked file into a temporary file and reports how big it is.
//
// The size has to be known before the first chunk is sent, because every header
// carries it and the receiver predicts each chunk's length from it. A picker on
// a phone hands the GUI a stream with no length, so the length is measured by
// copying once; streaming the copy is also what keeps a 200 MB video out of
// memory.
//
// The caller removes the returned path. The copy lives beside the finished
// files so it is on the same filesystem as everything else the app writes.
func stage(r io.Reader, dir string) (string, int64, error) {
	file, err := os.CreateTemp(dir, ".cthrone-send-*")
	if err != nil {
		return "", 0, fmt.Errorf("staging the picked file: %w", err)
	}

	path := file.Name()

	size, copyErr := io.Copy(file, r)
	closeErr := file.Close()

	if copyErr != nil {
		os.Remove(path)

		return "", 0, fmt.Errorf("staging the picked file: %w", copyErr)
	}

	if closeErr != nil {
		os.Remove(path)

		return "", 0, fmt.Errorf("staging the picked file: %w", closeErr)
	}

	return path, size, nil
}

// newTransferID names one transfer. It is random rather than sequential because
// two devices can be sending at the same time and the receiver keys transfers by
// this string alone.
func newTransferID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand does not fail on any platform this ships on; a fixed
		// name is still better than dropping the transfer.
		return "transfer"
	}

	return hex.EncodeToString(buf[:])
}

// percent is progress as a whole percentage, clamped to 0..100.
func percent(sent, total int64) int {
	if total <= 0 {
		return 100
	}

	p := int(sent * 100 / total)
	if p < 0 {
		return 0
	}

	if p > 100 {
		return 100
	}

	return p
}

// humanBytes renders a size the way a row has room for: 12 B, 2.4 KB, 1.1 GB.
func humanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// showsInline reports whether an arrival is drawn as a picture in the
// transcript, or as a file row with a Save action.
//
// The MIME type decides, because the name can lie and the receiver has already
// written the bytes: a type the toolkit cannot decode is a row, not an empty
// box.
func showsInline(mime string, size int64) bool {
	if size <= 0 || size > inlineImageMaxBytes {
		return false
	}

	kind := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = strings.TrimSpace(kind[:i])
	}

	return inlineTypes[kind]
}

// resolveDownloadDir is where a received file lands.
//
// It is deliberately not the configuration directory: that one holds the shared
// secret and the TLS key, and a download folder inside it would put a peer's
// file next to the one thing the user must not share. On a phone the app's own
// storage is the only folder that exists, so downloads live under it.
//
// It is the default a fresh install starts from: #114 added the preference that
// replaces it, and this is the fallback that preference falls back to.
func resolveDownloadDir(mobile bool, storageRoot string) string {
	if mobile {
		if storageRoot != "" {
			return filepath.Join(storageRoot, "downloads")
		}

		return filepath.Join(os.TempDir(), "cthrone", "downloads")
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Downloads")
	}

	return filepath.Join(os.TempDir(), "cthrone", "downloads")
}

// transferState is where a transfer row is in its life.
type transferState int

const (
	// transferSending is an outgoing file that is still on its way.
	transferSending transferState = iota

	// transferSent is an outgoing file the transport accepted in full.
	transferSent

	// transferSaved is an incoming file that arrived intact and is in the
	// folder the user chose.
	transferSaved

	// transferPending is an incoming file that arrived intact and is waiting
	// for the user to say where to put it. It is in the app's own storage, not
	// in a folder the user will find on their own.
	transferPending

	// transferFailed stopped on an error; the row says so instead of
	// pretending the file went anywhere.
	transferFailed
)

// fileNote is the file-shaped row in the transcript.
type fileNote struct {
	// id is the transfer's identity on the wire, and what a Cancel reaches for.
	// It is empty for a row that is not a live transfer.
	id    string
	name  string
	size  int64
	sent  int64
	state transferState
	// path is where a received file was written, and empty for an outgoing
	// one: the sender does not know the receiver's folder.
	path string
	// inline asks the row to draw the picture itself.
	inline bool
}

// line renders a transfer row.
func (n fileNote) line() string {
	switch n.state {
	case transferSent:
		return fmt.Sprintf("%s (%s) — sent", n.name, humanBytes(n.size))

	case transferSaved:
		if n.path != "" {
			return fmt.Sprintf("%s (%s) — saved to %s", n.name, humanBytes(n.size), n.path)
		}

		return fmt.Sprintf("%s (%s) — saved", n.name, humanBytes(n.size))

	case transferPending:
		return fmt.Sprintf("%s (%s) — waiting to be saved", n.name, humanBytes(n.size))

	case transferFailed:
		return n.name + " — failed"

	default:
		return fmt.Sprintf("%s — %d%% of %s", n.name, percent(n.sent, n.size), humanBytes(n.size))
	}
}

// dropFile removes the row that reports note, and reports whether it was there.
//
// A cancelled transfer leaves no row: the user asked for it to stop, so the
// transcript should not keep a line about something that did not happen. The
// row is compared by pointer, because the log is capped and an index recorded
// when the row was created can point at a different message by the time the
// transfer ends.
func dropFile(messages []chatMessage, note *fileNote) ([]chatMessage, bool) {
	for id := range messages {
		if messages[id].file == note {
			return append(messages[:id], messages[id+1:]...), true
		}
	}

	return messages, false
}

// showsText reports whether a transfer's body is drawn as text.
func showsText(mime string, size int64) bool {
	if size <= 0 || size > inlineTextBytes*4 {
		return false
	}

	kind := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = strings.TrimSpace(kind[:i])
	}

	return strings.HasPrefix(kind, textPreviewPrefix)
}

// textPreview reads the beginning of a text file, for the row that shows it.
//
// It is bounded twice: by bytes read, and by lines kept. A file that is not
// valid UTF-8 is not refused — a log with one bad byte is still worth reading —
// but a file that cannot be opened is, by returning "" and letting the row stay
// a file row.
func textPreview(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	body, err := io.ReadAll(io.LimitReader(file, inlineTextBytes))
	if err != nil || len(body) == 0 {
		return ""
	}

	text := strings.ToValidUTF8(string(body), "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")

	lines := strings.Split(text, "\n")
	truncated := len(body) == inlineTextBytes || len(lines) > inlineTextLines

	if len(lines) > inlineTextLines {
		lines = lines[:inlineTextLines]
	}

	text = strings.Join(lines, "\n")

	if truncated {
		text += "…"
	}

	return text
}
