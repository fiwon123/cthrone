package ui

import (
	"testing"

	"fyne.io/fyne/v2"
)

// A dialog's message box has to fit the window it is drawn in, and a window the
// size of a phone is the case that breaks it: the preferred width is wider than
// the canvas, so a naive box puts the dialog's own buttons off the screen.
func TestDialogTextSize(t *testing.T) {
	tests := []struct {
		name                  string
		canvas                fyne.Size
		wantWidth, wantHeight float32
	}{
		{
			name:       "no canvas yet uses the preferred size",
			canvas:     fyne.Size{},
			wantWidth:  dialogTextWidth,
			wantHeight: dialogTextHeight,
		},
		{
			name:       "a desktop window leaves the preferred size alone",
			canvas:     fyne.NewSize(1024, 768),
			wantWidth:  dialogTextWidth,
			wantHeight: dialogTextHeight,
		},
		{
			name:       "exactly at the margin still fits",
			canvas:     fyne.NewSize(dialogTextWidth+dialogMargin, dialogTextHeight+dialogMargin*2),
			wantWidth:  dialogTextWidth,
			wantHeight: dialogTextHeight,
		},
		{
			name:       "a phone is narrower than the preferred width",
			canvas:     fyne.NewSize(400, 800),
			wantWidth:  400 - dialogMargin,
			wantHeight: dialogTextHeight,
		},
		{
			name:       "a short landscape window shrinks the height instead",
			canvas:     fyne.NewSize(1024, 300),
			wantWidth:  dialogTextWidth,
			wantHeight: 300 - dialogMargin*2,
		},
		{
			name:       "a tiny canvas stops at the readable minimum",
			canvas:     fyne.NewSize(150, 120),
			wantWidth:  dialogTextMinWidth,
			wantHeight: dialogTextMinHeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dialogTextSize(tt.canvas)

			if got.Width != tt.wantWidth || got.Height != tt.wantHeight {
				t.Errorf("dialogTextSize(%v) = %v, want %v",
					tt.canvas, got, fyne.NewSize(tt.wantWidth, tt.wantHeight))
			}
		})
	}
}
