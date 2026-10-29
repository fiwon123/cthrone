package ui

// nextTabIndex is the tab a move of delta steps lands on.
//
// A tab bar is a ring: past the last tab is the first one, and before the first
// is the last. A window with no tabs (or one) has nowhere to go, and a current
// index outside the ring — nothing selected yet — starts from the first tab
// rather than panicking or landing somewhere arbitrary.
//
// It is a plain function because the keyboard handler around it needs a canvas
// and a window, and this is the part that can actually be wrong.
func nextTabIndex(current, count, delta int) int {
	if count <= 0 {
		return 0
	}

	if current < 0 || current >= count {
		current = 0
	}

	next := (current + delta) % count
	if next < 0 {
		next += count
	}

	return next
}
