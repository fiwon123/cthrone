//go:build gui

package ui

import (
	"io"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

// pickReader is the reader a picker hands over, with only the parts this
// package reads: the URI, a read that ends at once, and a close.
type pickReader struct {
	uri fyne.URI
}

func (p pickReader) URI() fyne.URI            { return p.uri }
func (p pickReader) Read([]byte) (int, error) { return 0, io.EOF }
func (p pickReader) Close() error             { return nil }

// contentURI is the shape a phone's picker hands over: a content:// document
// with no folder to remember (#303).
type contentURI struct{}

func (contentURI) String() string    { return "content://media/external/audio/media/42" }
func (contentURI) Extension() string { return "" }
func (contentURI) Name() string      { return "42" }
func (contentURI) MimeType() string  { return "audio/mpeg" }
func (contentURI) Scheme() string    { return "content" }
func (contentURI) Authority() string { return "media" }
func (contentURI) Path() string      { return "/external/audio/media/42" }
func (contentURI) Query() string     { return "" }
func (contentURI) Fragment() string  { return "" }

// The picker opens where the user last picked from, not in whatever the toolkit
// considers its places: on Windows that list can hold OneDrive or a
// disconnected share, and enumerating it before the first paint is what made
// Attach slow (#386).
func TestThePickerOpensWhereTheLastPickCameFrom(t *testing.T) {
	g := newScreenGUI(t, false)

	first := t.TempDir()
	second := t.TempDir()

	g.lastPick = first

	got := g.pickStart()
	if got == nil {
		t.Fatal("pickStart() returned nothing although the last folder exists")
	}

	if got.Path() != first {
		t.Errorf("pickStart() = %q, want the folder the last pick came from %q", got.Path(), first)
	}

	g.lastPick = second

	if got := g.pickStart(); got == nil || got.Path() != second {
		t.Errorf("pickStart() = %v, want the newest folder %q", got, second)
	}
}

// A folder that went away — an unplugged drive, a deleted folder — is not a
// start location: it falls through to the next one rather than opening a picker
// that cannot list anything.
func TestThePickerSkipsAFolderThatIsGone(t *testing.T) {
	g := newScreenGUI(t, false)

	gone := filepath.Join(t.TempDir(), "not-there")
	g.lastPick = gone

	got := g.pickStart()
	if got == nil {
		t.Fatal("pickStart() gave up although the download folder exists")
	}

	if got.Path() == gone {
		t.Error("pickStart() opened the folder that is gone")
	}
}

// A pick remembers its folder; a phone's content:// URI has none to remember
// and must not clear the desktop's.
func TestAPickRemembersItsFolder(t *testing.T) {
	g := newScreenGUI(t, false)

	dir := t.TempDir()
	g.rememberPick(pickReader{uri: storage.NewFileURI(filepath.Join(dir, "song.mp3"))})

	if g.lastPick != dir {
		t.Errorf("lastPick = %q, want the picked file's folder %q", g.lastPick, dir)
	}

	before := g.lastPick
	g.rememberPick(pickReader{uri: contentURI{}})

	if g.lastPick != before {
		t.Errorf("a content:// pick changed the remembered folder to %q", g.lastPick)
	}
}

// The remembered folder is not required to exist for the picker to work: a
// fresh window has none, and the folder received files land in — or the home
// folder — is the answer.
func TestAFreshWindowStartsWithAFolderItKnows(t *testing.T) {
	g := newScreenGUI(t, false)

	if g.lastPick != "" {
		t.Fatalf("a fresh window remembers %q", g.lastPick)
	}

	if g.pickStart() == nil {
		t.Error("pickStart() has nothing to open in a fresh window")
	}
}
