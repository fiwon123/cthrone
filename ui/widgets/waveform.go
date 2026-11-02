package widgets

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// waveformHeight is how tall the shape sits in a transcript row: tall enough to
// see a word's shape, short enough that three of them still fit above a phone's
// keyboard.
const waveformHeight = 36

// Waveform is a seek control for a clip: the shape of the sound with a
// playhead, tapped or dragged to move through it.
//
// It draws rather than holds a widget per sample: a row in a list is painted on
// every repaint, so the shape is one raster over a slice of peaks this package
// is handed, never a tree of rectangles (#399).
type Waveform struct {
	widget.BaseWidget

	peaks    []float32
	position float32
	onSeek   func(float32)

	raster *canvas.Raster
}

// NewWaveform makes a seek control; onSeek runs with the fraction of the clip
// the user chose, 0 at the start and 1 at the end.
func NewWaveform(onSeek func(float32)) *Waveform {
	w := &Waveform{onSeek: onSeek}
	w.ExtendBaseWidget(w)

	w.raster = canvas.NewRaster(w.shape)

	return w
}

// SetPeaks hands the control the clip's shape: one value per column, 0 for
// silence and 1 for the loudest sample.
func (w *Waveform) SetPeaks(peaks []float32) {
	w.peaks = peaks
	w.raster.Refresh()
}

// Peaks is the shape as it was last set, for a test or a repaint to read.
func (w *Waveform) Peaks() []float32 { return w.peaks }

// SetFraction moves the playhead, clamped to the clip.
//
// It is not called SetPosition: a canvas object already has one, and that one
// is a point on the screen.
func (w *Waveform) SetFraction(position float32) {
	w.position = clampFraction(position)
	w.raster.Refresh()
}

// Fraction is the playhead as a fraction of the clip.
func (w *Waveform) Fraction() float32 { return w.position }

// SetOnSeek replaces the callback a tap or a drag runs.
func (w *Waveform) SetOnSeek(onSeek func(float32)) { w.onSeek = onSeek }

// MinSize is a width a row can give and the shape's height.
func (w *Waveform) MinSize() fyne.Size {
	w.ExtendBaseWidget(w)

	return fyne.NewSize(160, waveformHeight)
}

// CreateRenderer draws the raster; the widget itself holds no layout.
func (w *Waveform) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.raster)
}

// Tapped seeks to the pointer.
func (w *Waveform) Tapped(event *fyne.PointEvent) {
	w.seekAt(event.Position.X)
}

// Dragged seeks as the pointer moves: the point of a seek bar is that the time
// follows the finger, not that it is chosen once.
func (w *Waveform) Dragged(event *fyne.DragEvent) {
	w.seekAt(event.Position.X)
}

// DragEnd finishes a drag; the position has already followed it.
func (w *Waveform) DragEnd() {}

// seekAt turns an x inside the control into a fraction of the clip.
func (w *Waveform) seekAt(x float32) {
	width := w.Size().Width
	if width <= 0 {
		return
	}

	w.SetFraction(x / width)

	if w.onSeek != nil {
		w.onSeek(w.position)
	}
}

// shape draws the waveform and the playhead at the size the row gave.
//
// A column is one pixel: peaks are resampled to the width by index, which is
// enough for a shape a thumb aims at and cheap enough to redraw on every
// position change.
func (w *Waveform) shape(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	if width <= 0 || height <= 0 {
		return img
	}

	th := w.Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()

	played := th.Color(theme.ColorNamePrimary, variant)
	idle := dimmed(th.Color(theme.ColorNameForeground, variant))
	head := th.Color(theme.ColorNamePrimary, variant)

	mid := height / 2
	playedUntil := int(w.position * float32(width))

	// A baseline, so an all-silent clip is still a control and not an empty
	// row.
	for x := 0; x < width; x++ {
		img.Set(x, mid, idle)
	}

	for x := 0; x < width; x++ {
		peak := w.peakAt(x, width)

		half := int(peak * float32(mid))

		bar := idle
		if x <= playedUntil {
			bar = played
		}

		for y := mid - half; y <= mid+half; y++ {
			if y >= 0 && y < height {
				img.Set(x, y, bar)
			}
		}
	}

	// The playhead: a full-height line where the sound has got to.
	for y := 0; y < height; y++ {
		x := playedUntil
		if x >= 0 && x < width {
			img.Set(x, y, head)
		}
	}

	return img
}

// peakAt reads the peak for a column, resampling the shape to the width.
func (w *Waveform) peakAt(x, width int) float32 {
	if len(w.peaks) == 0 {
		return 0
	}

	if len(w.peaks) == width {
		return clampFraction(w.peaks[x])
	}

	i := x * len(w.peaks) / width
	if i >= len(w.peaks) {
		i = len(w.peaks) - 1
	}

	return clampFraction(w.peaks[i])
}

// dimmed is the same colour at a quarter of its alpha: the part of the clip
// that has not been played is visible, and does not compete with the playhead.
func dimmed(c color.Color) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = n.A / 4

	return n
}

// clampFraction keeps a fraction inside the clip.
func clampFraction(f float32) float32 {
	if f < 0 {
		return 0
	}

	if f > 1 {
		return 1
	}

	return f
}
