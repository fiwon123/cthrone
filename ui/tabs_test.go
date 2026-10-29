package ui

import "testing"

// TestNextTabIndex covers the ring: both directions, both ends, and the sizes
// where a move has nowhere to go.
func TestNextTabIndex(t *testing.T) {
	tests := []struct {
		name          string
		current, size int
		delta         int
		want          int
	}{
		{"forward from the first", 0, 3, 1, 1},
		{"forward from the middle", 1, 3, 1, 2},
		{"forward wraps to the first", 2, 3, 1, 0},
		{"back from the first wraps to the last", 0, 3, -1, 2},
		{"back from the middle", 1, 3, -1, 0},
		{"back from the last", 2, 3, -1, 1},
		{"no move", 1, 3, 0, 1},
		{"more than a full lap forwards", 1, 3, 4, 2},
		{"more than a full lap backwards", 1, 3, -4, 0},
		{"one tab has nowhere to go", 0, 1, 1, 0},
		{"one tab, backwards", 0, 1, -1, 0},
		{"no tabs", 0, 0, 1, 0},
		{"no tabs, backwards", 0, 0, -1, 0},
		{"nothing selected yet, forwards", -1, 3, 1, 1},
		{"nothing selected yet, backwards", -1, 3, -1, 2},
		{"index past the end", 7, 3, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextTabIndex(tt.current, tt.size, tt.delta); got != tt.want {
				t.Errorf("nextTabIndex(%d, %d, %d) = %d, want %d",
					tt.current, tt.size, tt.delta, got, tt.want)
			}
		})
	}
}

// TestNextTabIndexStaysInRange walks a ring from every start in both
// directions: whatever the arithmetic, the answer has to be a tab that exists.
func TestNextTabIndexStaysInRange(t *testing.T) {
	for size := 1; size <= 5; size++ {
		for current := 0; current < size; current++ {
			for delta := -2 * size; delta <= 2*size; delta++ {
				got := nextTabIndex(current, size, delta)
				if got < 0 || got >= size {
					t.Fatalf("nextTabIndex(%d, %d, %d) = %d, outside [0, %d)",
						current, size, delta, got, size)
				}
			}
		}
	}
}
