//go:build gui

package ui

import (
	"image"
	"testing"

	"fyne.io/fyne/v2"
)

// TestWindowPaintsItsContent is the assertion the v0.8.3 release lacked: the
// window is built, sized and reported visible, yet the toolkit paints a single
// flat colour — which is what "nothing appears on screen" looks like.
//
// The cause was the window's root content: widgets.Tabs embedded a container
// without implementing CreateRenderer, so the paint walk could not enter it and
// skipped the entire tree (#186). Both layouts are checked because the report
// named the phone as well as the desktop.
func TestWindowPaintsItsContent(t *testing.T) {
	tests := []struct {
		name   string
		mobile bool
		size   fyne.Size
	}{
		{"desktop", false, fyne.NewSize(900, 640)},
		{"phone", true, fyne.NewSize(360, 640)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newScreenGUI(t, tt.mobile)

			g.win.Resize(tt.size)

			seen := paintedColours(t, g.win.Canvas())
			if seen < 2 {
				t.Fatalf("the window painted %d colour(s): the screen is blank", seen)
			}
		})
	}
}

// paintedColours counts the distinct pixel values a canvas would draw. One
// colour means only the background reached the screen.
func paintedColours(t *testing.T, c fyne.Canvas) int {
	t.Helper()

	canvas, ok := c.(interface{ Capture() image.Image })
	if !ok {
		t.Fatalf("canvas %T cannot capture", c)
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
