package widgets

import (
	"image"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// These tests assert pixels, not widget state.
//
// A window can be built, sized and "visible" while the toolkit paints a single
// flat colour: the paint walk enters a *fyne.Container or a fyne.Widget and
// treats anything else as a leaf that draws nothing. The types in this package
// embed a container, so each one needs CreateRenderer — and each one needs a
// test that would fail without it, which a state assertion cannot do.

// paintedColours renders obj on a test canvas and counts the distinct pixel
// values. A number below two means the background is all that reached the
// screen.
func paintedColours(t *testing.T, obj fyne.CanvasObject, size fyne.Size) int {
	t.Helper()

	w := test.NewWindow(obj)
	t.Cleanup(w.Close)

	w.Resize(size)

	canvas, ok := w.Canvas().(interface{ Capture() image.Image })
	if !ok {
		t.Fatalf("canvas %T cannot capture", w.Canvas())
	}

	img := canvas.Capture()
	if img == nil {
		t.Fatal("the capture is nil")
	}

	seen := make(map[uint32]struct{})

	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, a := img.At(x, y).RGBA()
			seen[uint32(r)<<24|uint32(g)<<16|uint32(bb)<<8|uint32(a)>>8] = struct{}{}
		}
	}

	return len(seen)
}

func newPaintApp(t *testing.T) {
	t.Helper()

	app := test.NewApp()
	t.Cleanup(app.Quit)
}

// TestCopyButtonPaintsItsLabel is the regression from #181/#186: a button that
// is a container wrapper drew nothing at all, so every action in the app was
// invisible even though it still worked when tapped blind.
func TestCopyButtonPaintsItsLabel(t *testing.T) {
	newPaintApp(t)

	button := NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), nil, nil)

	if got := paintedColours(t, button, fyne.NewSize(240, 80)); got < 2 {
		t.Fatalf("the button painted %d colour(s): it is invisible", got)
	}
}

// TestTabsPaintsTheSelectedPane covers the other half: the tab bar is the
// window's root content in the app, and a bar the walk cannot enter leaves the
// whole screen blank.
func TestTabsPaintsTheSelectedPane(t *testing.T) {
	newPaintApp(t)

	tabs := NewTabs(false, nil,
		container.NewTabItem("Chat", widget.NewLabel("the chat pane")),
		container.NewTabItem("Setup", widget.NewLabel("the setup pane")),
	)

	if got := paintedColours(t, tabs, fyne.NewSize(480, 320)); got < 2 {
		t.Fatalf("the tab bar painted %d colour(s): it is invisible", got)
	}

	tabs.SelectIndex(1)

	if got := paintedColours(t, tabs, fyne.NewSize(480, 320)); got < 2 {
		t.Fatalf("the second tab painted %d colour(s): the pane did not appear", got)
	}
}

// TestCheckRowPaintsItsCaption keeps the Setup tab honest: the check rows are
// the only way to change the transport and the TLS options.
func TestCheckRowPaintsItsCaption(t *testing.T) {
	newPaintApp(t)

	row := NewCheckRow("Encrypt with TLS", nil, true)

	if got := paintedColours(t, row, fyne.NewSize(360, 80)); got < 2 {
		t.Fatalf("the check row painted %d colour(s): it is invisible", got)
	}
}
