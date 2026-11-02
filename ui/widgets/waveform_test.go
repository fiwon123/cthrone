package widgets

import (
	"image"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// seekable is a waveform in a test app, sized like a row would size it.
func seekable(t *testing.T, onSeek func(float32)) *Waveform {
	t.Helper()

	app := test.NewApp()
	t.Cleanup(app.Quit)

	w := NewWaveform(onSeek)
	w.Resize(fyne.NewSize(200, waveformHeight))

	return w
}

// A tap chooses the fraction of the clip the pointer is over, and a drag keeps
// choosing as it moves — the time follows the finger (#399).
func TestWaveformSeekFollowsThePointer(t *testing.T) {
	var seeks []float32

	w := seekable(t, func(f float32) { seeks = append(seeks, f) })

	test.TapAt(w, fyne.NewPos(50, waveformHeight/2)) // a quarter of 200

	if len(seeks) != 1 {
		t.Fatalf("%d seeks from a tap, want 1", len(seeks))
	}

	if seeks[0] != 0.25 {
		t.Errorf("a tap at 25%% sought to %v, want 0.25", seeks[0])
	}

	w.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(100, 10)}})
	w.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(200, 10)}})

	if len(seeks) != 3 {
		t.Fatalf("%d seeks after two drags, want 3", len(seeks))
	}

	if seeks[1] != 0.5 || seeks[2] != 1 {
		t.Errorf("the drags sought to %v, want 0.5 then 1", seeks[1:])
	}

	if got := w.Fraction(); got != 1 {
		t.Errorf("the playhead is at %v after the drag, want the end", got)
	}
}

// A pointer outside the control is clamped to the clip: a drag past the end is
// the end, not a position past it.
func TestWaveformClampsTheSeek(t *testing.T) {
	var last float32 = -1

	w := seekable(t, func(f float32) { last = f })

	test.TapAt(w, fyne.NewPos(-30, 10))

	if last != 0 {
		t.Errorf("a tap before the start sought to %v, want 0", last)
	}

	test.TapAt(w, fyne.NewPos(500, 10))

	if last != 1 {
		t.Errorf("a tap past the end sought to %v, want 1", last)
	}
}

// The shape is drawn from the peaks: a loud column reaches the top and bottom
// of the control, silence leaves the baseline alone.
func TestWaveformDrawsThePeaks(t *testing.T) {
	w := seekable(t, nil)
	w.SetPeaks([]float32{0, 0, 1, 0})

	img := w.raster.Generator(4, waveformHeight)

	// Column 2 is the loud one: its top row is painted, and a silent column's
	// is not. Column 0 is skipped because the playhead sits on it at 0.
	if _, _, _, a := img.At(2, 0).RGBA(); a == 0 {
		t.Error("the loud column did not reach the top of the control")
	}

	if _, _, _, a := img.At(1, 0).RGBA(); a != 0 {
		t.Error("a silent column painted the top of the control")
	}

	// The baseline is always there: a silent clip is still a control.
	if _, _, _, a := img.At(0, waveformHeight/2).RGBA(); a == 0 {
		t.Error("the baseline is missing, so a silent clip draws nothing")
	}
}

// The playhead is visible where the sound has got to, and moving it repaints.
func TestWaveformMarksThePlayhead(t *testing.T) {
	w := seekable(t, nil)
	w.SetPeaks([]float32{1, 1, 1, 1})

	start := w.raster.Generator(4, waveformHeight)
	before := colourAt(start, 2, 0)

	w.SetFraction(0.5)

	after := colourAt(w.raster.Generator(4, waveformHeight), 2, 0)

	if before == after {
		t.Error("moving the playhead changed nothing on the control")
	}
}

// colourAt is one pixel as a comparable value.
func colourAt(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()

	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}
