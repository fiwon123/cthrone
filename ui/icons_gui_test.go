//go:build gui

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/ui/widgets"
)

// A file row says what kind of file it is, so a PDF and a song are not the same
// line of text (#304). This toolkit has no PDF icon, so a PDF takes the generic
// document.
func TestFileIconFollowsTheKind(t *testing.T) {
	tests := []struct {
		name string
		want fyne.Resource
	}{
		{"song.mp3", theme.FileAudioIcon()},
		{"memo.m4a", theme.FileAudioIcon()},
		{"clip.mp4", theme.FileVideoIcon()},
		{"photo.jpg", theme.FileImageIcon()},
		{"notes.txt", theme.FileTextIcon()},
		{"report.pdf", theme.FileIcon()},
		{"archive.zip", theme.FileApplicationIcon()},
		{"no-extension", theme.FileApplicationIcon()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileIconFor(tt.name); got != tt.want {
				t.Errorf("fileIconFor(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// A rename that only changes the case must not change the icon: a phone's
// keyboard capitalises, and the extension arrives as "JPG".
func TestFileIconIgnoresTheCaseOfTheExtension(t *testing.T) {
	if got := fileIconFor("HOLIDAY.JPG"); got != theme.FileImageIcon() {
		t.Errorf("fileIconFor(HOLIDAY.JPG) = %v, want the image icon", got)
	}
}

// fillRowOnScreen puts one file row through the binding the list calls and
// returns its parts.
func fillRowOnScreen(t *testing.T, g *gui, note *fileNote) (*canvas.Image, *widgets.Text, *widget.Icon) {
	t.Helper()

	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", file: note}}
	g.mu.Unlock()

	pic := canvas.NewImageFromResource(nil)
	preview := g.text("")
	icon := widget.NewIcon(theme.FileIcon())
	save := widgets.NewButton("Save as…", nil, nil)
	open := widgets.NewButtonWithIcon("Open", nil, nil, nil)

	g.fillFileRow(chatMessage{from: "laptop", file: note}, pic, preview, icon, save, open, nil)

	return pic, preview, icon
}

// A row that draws nothing shows its type: that icon is the only thing telling a
// PDF from an audio clip (#304).
func TestAFileRowThatDrawsNothingShowsItsType(t *testing.T) {
	g := newScreenGUI(t, false)

	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 not really"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, icon := fillRowOnScreen(t, g, &fileNote{id: "id-1", name: "report.pdf", size: 16, state: transferSaved, path: path})

	if !icon.Visible() {
		t.Fatal("a PDF row draws no icon, so nothing says what kind of file it is")
	}

	if icon.Resource != theme.FileIcon() {
		t.Errorf("the PDF icon is %v, want the generic document", icon.Resource)
	}
}

// A row that draws its own content does not need to say it is a picture: the
// picture is the answer.
func TestARowThatDrawsItsContentShowsNoIcon(t *testing.T) {
	decodePicturesInline(t)

	g := newScreenGUI(t, false)

	path := writeTestPNG(t)

	pic, _, icon := fillRowOnScreen(t, g, &fileNote{
		id: "id-1", name: "photo.png", size: testFileSize(t, path),
		state: transferSaved, path: path, inline: true,
	})

	if !pic.Visible() {
		t.Fatal("the picture row drew nothing")
	}

	if icon.Visible() {
		t.Error("a row drawing its picture also drew a type icon")
	}
}

// A text file drawn in its row is content too: no icon over it.
func TestATextRowShowsNoIcon(t *testing.T) {
	g := newScreenGUI(t, false)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("the minutes of the meeting\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, preview, icon := fillRowOnScreen(t, g, &fileNote{
		id: "id-1", name: "notes.txt", size: 26, state: transferSaved, path: path,
	})

	if !preview.Visible() {
		t.Fatal("the text row did not draw its content")
	}

	if icon.Visible() {
		t.Error("a row drawing its text also drew a type icon")
	}
}

// A phone cannot hand a private file to another app, so the row must not offer a
// button that can only fail: Save as… is the way out, and it uses the system's
// own file dialog (#305).
func TestAPhoneOffersNoOpenButton(t *testing.T) {
	previous := canOpenFiles
	canOpenFiles = func() bool { return false }
	t.Cleanup(func() { canOpenFiles = previous })

	g := newScreenGUI(t, true)

	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 not really"), 0o600); err != nil {
		t.Fatal(err)
	}

	note := &fileNote{id: "id-1", name: "report.pdf", size: 16, state: transferSaved, path: path}

	pic := canvas.NewImageFromResource(nil)
	preview := g.text("")
	icon := widget.NewIcon(theme.FileIcon())
	save := widgets.NewButton("Save as…", nil, nil)
	open := widgets.NewButtonWithIcon("Open", nil, nil, nil)

	g.fillFileRow(chatMessage{from: "laptop", file: note}, pic, preview, icon, save, open, nil)

	if open.Visible() {
		t.Error("a phone offered an Open button that cannot work")
	}

	if !save.Visible() {
		t.Error("a phone hid Save as…, which is the only way to get the file out")
	}
}

// A desktop keeps Open: the file is on its disk and the system opener can take
// it.
func TestADesktopKeepsTheOpenButton(t *testing.T) {
	previous := canOpenFiles
	canOpenFiles = func() bool { return true }
	t.Cleanup(func() { canOpenFiles = previous })

	g := newScreenGUI(t, false)

	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 not really"), 0o600); err != nil {
		t.Fatal(err)
	}

	note := &fileNote{id: "id-1", name: "report.pdf", size: 16, state: transferSaved, path: path}

	pic := canvas.NewImageFromResource(nil)
	preview := g.text("")
	icon := widget.NewIcon(theme.FileIcon())
	save := widgets.NewButton("Save as…", nil, nil)
	open := widgets.NewButtonWithIcon("Open", nil, nil, nil)

	g.fillFileRow(chatMessage{from: "laptop", file: note}, pic, preview, icon, save, open, nil)

	if !open.Visible() {
		t.Error("a desktop hid the Open button, which works there")
	}
}

// Painting a row must not decode its picture again: the list repaints visible
// rows on every message, so a transcript with pictures got slower the more was
// said (#321).
func TestARowDecodesItsPictureOnce(t *testing.T) {
	g := newScreenGUI(t, false)

	path := writeTestPNG(t)

	// The row's decode is the thumbnail worker now, so that is what is counted:
	// one decode per path, however many times the row is painted (#321, #510).
	decodePicturesInline(t)

	loads := 0
	previous := decodeThumb
	decodeThumb = func(p string) (fyne.Resource, error) {
		loads++

		return previous(p)
	}
	t.Cleanup(func() { decodeThumb = previous })

	checks := 0
	previousCheck := canDecodeFile
	canDecodeFile = func(p string) bool {
		checks++

		return previousCheck(p)
	}
	t.Cleanup(func() { canDecodeFile = previousCheck })

	note := &fileNote{id: "id-1", name: "photo.png", size: testFileSize(t, path), state: transferSaved, path: path, inline: true}

	for paint := 1; paint <= 3; paint++ {
		pic, preview, icon := fillRowOnScreen(t, g, note)
		if !pic.Visible() && preview.Visible() {
			t.Fatalf("paint %d drew neither picture nor text", paint)
		}
		_ = icon
	}

	if loads != 1 {
		t.Errorf("the picture was decoded %d times over three paints, want 1", loads)
	}

	if checks != 1 {
		t.Errorf("the decodability was probed %d times over three paints, want 1", checks)
	}
}

// The same rule for a text file: read once, painted many times (#321).
func TestATextRowReadsItsFileOnce(t *testing.T) {
	g := newScreenGUI(t, false)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("the minutes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reads := 0
	previous := readText
	readText = func(p string) string {
		reads++

		return previous(p)
	}
	t.Cleanup(func() { readText = previous })

	note := &fileNote{id: "id-1", name: "notes.txt", size: 12, state: transferSaved, path: path}

	for paint := 1; paint <= 3; paint++ {
		fillRowOnScreen(t, g, note)
	}

	if reads != 1 {
		t.Errorf("the text was read %d times over three paints, want 1", reads)
	}
}
