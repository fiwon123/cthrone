package ui

import "testing"

func TestLayoutFor(t *testing.T) {
	tests := []struct {
		name      string
		mobile    bool
		width     int
		wantStack bool
		wantMin   int
	}{
		{"desktop, wide", false, 900, false, 320},
		{"desktop, unmeasured", false, 0, false, 320},
		{"desktop, narrow window", false, 380, true, 0},
		{"desktop, at the threshold", false, narrowWidth, false, 320},
		{"phone, unmeasured", true, 0, true, 0},
		{"phone, 360dp", true, 360, true, 0},
		{"phone, very narrow", true, 280, true, 0},
		// A phone stays stacked even sideways: the two-column layout is a
		// desktop's, and a rotation must not bring it back (#302).
		{"phone, landscape", true, 640, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := layoutFor(tt.mobile, tt.width)

			if got.Stack != tt.wantStack {
				t.Errorf("layoutFor(%v, %d).Stack = %v, want %v", tt.mobile, tt.width, got.Stack, tt.wantStack)
			}
			if got.ChatMinWidth != tt.wantMin {
				t.Errorf("layoutFor(%v, %d).ChatMinWidth = %d, want %d", tt.mobile, tt.width, got.ChatMinWidth, tt.wantMin)
			}
		})
	}
}

// TestLayoutKeepsANarrowScreenInsideTheWindow guards the reason the rule
// exists: a narrow screen must never be given a minimum width, because that is
// what made the page wider than the phone.
func TestLayoutKeepsANarrowScreenInsideTheWindow(t *testing.T) {
	for _, width := range []int{0, 280, 320, 360, 419} {
		if got := layoutFor(true, width).ChatMinWidth; got != 0 {
			t.Errorf("layoutFor(true, %d).ChatMinWidth = %d, want 0", width, got)
		}
	}
}
