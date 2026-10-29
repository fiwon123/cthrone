package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout is how long a transfer may go quiet before it is discarded.
// It is generous on purpose: a slow phone on a weak link is still a transfer,
// while a peer that disconnected is noticed within two minutes.
const DefaultTimeout = 2 * time.Minute

// maxCollisions bounds the suffix search when a file name is already taken.
const maxCollisions = 1000

// Assembler turns the frames of an incoming file back into a file on disk.
//
// It writes into a temporary file in Dir and only renames it to the name the
// sender chose once the size and the SHA-256 match. So an interrupted transfer
// leaves a dot-prefixed part file in the download folder, never a truncated
// document the user finds later and trusts.
//
// An Assembler is not safe for concurrent use: a transport delivers one frame
// at a time, and reading them in order is part of what makes a transfer
// verifiable.
type Assembler struct {
	// Dir is where finished files land and where part files live while they
	// are being assembled. Empty means the operating system's temporary
	// directory.
	Dir string

	// Timeout is how long a transfer may go quiet. Zero or negative means
	// DefaultTimeout.
	Timeout time.Duration

	// now is the clock, so a test can make a transfer stale without sleeping.
	now       func() time.Time
	transfers map[string]*transfer
}

// transfer is one file being assembled.
type transfer struct {
	header   Header // the first chunk's header: name, size, chunk count, MIME
	path     string // the part file
	file     *os.File
	hash     hash.Hash
	received int64
	next     int // the index that must arrive next
	last     time.Time
}

// File is a transfer that arrived intact.
type File struct {
	Name string
	Path string
	Size int64
	MIME string
}

// NewAssembler makes an assembler that writes into dir and forgets a transfer
// that has been quiet for timeout.
func NewAssembler(dir string, timeout time.Duration) *Assembler {
	if dir == "" {
		dir = os.TempDir()
	}

	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Assembler{
		Dir:       dir,
		Timeout:   timeout,
		now:       time.Now,
		transfers: make(map[string]*transfer),
	}
}

// Add feeds one frame to the assembler.
//
// A chunk extends its transfer and returns no file. A file-end closes it: the
// size and hash are checked, the part file is renamed to the sender's name, and
// the finished File is returned. Text is not a transfer and returns
// ErrNotTransfer, so a caller can hand every parsed frame here and still be
// told when it did not make sense.
//
// Any malformed, out-of-order or dishonest frame discards its whole transfer:
// once a stream is broken, the rest of it cannot be trusted, and a part file
// kept for later is a part file kept forever.
func (a *Assembler) Add(f Frame) (*File, error) {
	if a == nil {
		return nil, errors.New("wire: nil assembler")
	}

	a.init()

	if err := f.Header.validate(); err != nil {
		return nil, err
	}

	if err := f.Header.checkPayload(len(f.Payload)); err != nil {
		return nil, err
	}

	a.Sweep()

	switch f.Header.Kind {
	case KindFile:
		return nil, a.addChunk(f)

	case KindFileEnd:
		return a.finish(f.Header)

	default:
		return nil, fmt.Errorf("%w: %q is not a file transfer", ErrNotTransfer, f.Header.Kind)
	}
}

// Sweep discards every transfer that has been quiet for longer than the
// timeout and reports how many it dropped. Add calls it, so a caller with no
// ticker of its own still reaps; a caller with one can say so explicitly.
func (a *Assembler) Sweep() int {
	if a == nil {
		return 0
	}

	a.init()

	now := a.now()
	dropped := 0

	for id, t := range a.transfers {
		if now.Sub(t.last) <= a.Timeout {
			continue
		}

		a.drop(id)
		dropped++
	}

	return dropped
}

// Close discards every transfer in flight. It is what a caller does when the
// session ends: there will be no more chunks, and a part file left behind is
// the one thing this type exists to avoid.
func (a *Assembler) Close() {
	if a == nil {
		return
	}

	for id := range a.transfers {
		a.drop(id)
	}
}

// init fills in a zero-value Assembler, so NewAssembler is a convenience and
// not a requirement.
func (a *Assembler) init() {
	if a.Dir == "" {
		a.Dir = os.TempDir()
	}

	if a.Timeout <= 0 {
		a.Timeout = DefaultTimeout
	}

	if a.now == nil {
		a.now = time.Now
	}

	if a.transfers == nil {
		a.transfers = make(map[string]*transfer)
	}
}

// addChunk extends a transfer, starting it when this is its first chunk.
func (a *Assembler) addChunk(f Frame) error {
	h := f.Header
	t := a.transfers[h.ID]

	if t == nil {
		if h.Index != 0 {
			return fmt.Errorf("%w: %q starts at chunk %d, not 0", ErrSequence, h.ID, h.Index)
		}

		started, err := a.start(h)
		if err != nil {
			return err
		}

		t = started
	} else if err := t.check(h); err != nil {
		a.drop(h.ID)

		return err
	}

	if _, err := t.file.Write(f.Payload); err != nil {
		a.drop(h.ID)

		return fmt.Errorf("wire: writing %q: %w", h.ID, err)
	}

	t.hash.Write(f.Payload) // hash.Hash never returns an error
	t.received += int64(len(f.Payload))
	t.next++
	t.last = a.now()

	return nil
}

// start opens the part file a transfer is written into.
func (a *Assembler) start(h Header) (*transfer, error) {
	file, err := os.CreateTemp(a.Dir, ".cthrone-part-*")
	if err != nil {
		return nil, fmt.Errorf("wire: opening a part file: %w", err)
	}

	t := &transfer{
		header: h,
		path:   file.Name(),
		file:   file,
		hash:   sha256.New(),
		last:   a.now(),
	}
	a.transfers[h.ID] = t

	return t, nil
}

// check reports whether h can be the next frame of t.
func (t *transfer) check(h Header) error {
	if h.Name != t.header.Name || h.Size != t.header.Size || h.Chunks != t.header.Chunks {
		return fmt.Errorf("%w: chunk %d of %q describes a different file", ErrHeader, h.Index, h.ID)
	}

	if h.Index != t.next {
		return fmt.Errorf("%w: chunk %d of %q arrived where %d was due", ErrSequence, h.Index, h.ID, t.next)
	}

	return nil
}

// finish closes a transfer: checks what arrived against what was promised,
// renames the part file, and returns it.
func (a *Assembler) finish(h Header) (*File, error) {
	t := a.transfers[h.ID]
	if t == nil {
		// A zero-byte file has no chunks, so its file-end is the whole
		// transfer. Anything else ending here never started.
		if h.Size == 0 {
			return a.finishEmpty(h)
		}

		return nil, fmt.Errorf("%w: %q ended without a chunk", ErrSequence, h.ID)
	}

	if h.Size != t.header.Size || h.Chunks != t.header.Chunks || h.Name != t.header.Name {
		a.drop(h.ID)

		return nil, fmt.Errorf("%w: file-end for %q describes a different file", ErrHeader, h.ID)
	}

	if t.next != h.Chunks || t.received != h.Size {
		a.drop(h.ID)

		return nil, fmt.Errorf("%w: %q ended after %d of %d chunks and %d of %d bytes",
			ErrIntegrity, h.ID, t.next, h.Chunks, t.received, h.Size)
	}

	if !strings.EqualFold(hex.EncodeToString(t.hash.Sum(nil)), h.SHA256) {
		a.drop(h.ID)

		return nil, fmt.Errorf("%w: %q arrived with a different hash than the header", ErrIntegrity, h.ID)
	}

	if err := t.file.Close(); err != nil {
		a.drop(h.ID)

		return nil, fmt.Errorf("wire: closing %q: %w", h.ID, err)
	}

	delete(a.transfers, h.ID)

	path, err := a.uniquePath(t.header.Name)
	if err != nil {
		os.Remove(t.path)

		return nil, err
	}

	if err := os.Rename(t.path, path); err != nil {
		os.Remove(t.path)

		return nil, fmt.Errorf("wire: moving %q into place: %w", h.ID, err)
	}

	return &File{Name: t.header.Name, Path: path, Size: h.Size, MIME: t.header.MIME}, nil
}

// finishEmpty writes a zero-byte file, whose hash is the hash of nothing.
func (a *Assembler) finishEmpty(h Header) (*File, error) {
	if !strings.EqualFold(SumHex(sha256.Sum256(nil)), h.SHA256) {
		return nil, fmt.Errorf("%w: empty %q arrived with a different hash than the header", ErrIntegrity, h.ID)
	}

	file, err := os.CreateTemp(a.Dir, ".cthrone-part-*")
	if err != nil {
		return nil, fmt.Errorf("wire: opening a part file: %w", err)
	}

	part := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(part)

		return nil, fmt.Errorf("wire: closing the part file: %w", err)
	}

	path, err := a.uniquePath(h.Name)
	if err != nil {
		os.Remove(part)

		return nil, err
	}

	if err := os.Rename(part, path); err != nil {
		os.Remove(part)

		return nil, fmt.Errorf("wire: moving the empty file into place: %w", err)
	}

	return &File{Name: h.Name, Path: path, Size: 0, MIME: h.MIME}, nil
}

// drop forgets a transfer and takes its part file with it.
func (a *Assembler) drop(id string) {
	t := a.transfers[id]
	if t == nil {
		return
	}

	delete(a.transfers, id)
	t.file.Close()
	os.Remove(t.path)
}

// uniquePath picks where a finished file goes, without replacing an earlier
// one: two files called photo.jpg are two files, and the second becomes
// "photo (1).jpg".
//
// The naming policy is provisional until the save-policy issue decides whether
// a collision should ask instead; the rule this type must keep either way is
// that a file already in the folder is never overwritten silently.
func (a *Assembler) uniquePath(name string) (string, error) {
	for i := 0; i <= maxCollisions; i++ {
		candidate := filepath.Join(a.Dir, numbered(name, i))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("wire: no free name for %q in %s", name, a.Dir)
}

// numbered is name for i == 0 and "stem (i).ext" after that.
func numbered(name string, i int) string {
	if i == 0 {
		return name
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	return fmt.Sprintf("%s (%d)%s", stem, i, ext)
}
