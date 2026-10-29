package ui

// narrowWidth is the canvas width, in device-independent pixels, below which a
// row of controls no longer fits side by side. A phone is commonly 360dp wide,
// and a checkbox label next to a button in one line overflows that easily.
const narrowWidth = 420

// screenLayout records how a screen of the given size arranges the rows the GUI
// shares between the desktop and the phone. The fields are plain numbers and
// bools so the rule can be tested without a canvas, and so the package keeps
// building without the gui tag.
type screenLayout struct {
	// Stack puts the controls of a row on separate lines when the row is too
	// narrow to hold them side by side.
	Stack bool
	// ActionColumns is how many buttons share one line in the chat action
	// grid. Zero keeps the desktop arrangement, with the buttons beside the
	// message field instead of under it.
	ActionColumns int
	// ChatMinWidth is the smallest width the transcript is allowed to take.
	// Zero leaves the width to the screen, so a narrow page is never wider
	// than the window it is drawn in.
	ChatMinWidth int
}

// layoutFor decides the arrangement for a canvas of the given width. A width
// of zero means the canvas has not been measured yet: a phone is then assumed
// to be narrow, because stacking early only costs a little space while failing
// to stack pushes content off the screen.
func layoutFor(mobile bool, width int) screenLayout {
	narrow := width > 0 && width < narrowWidth
	if mobile && width <= 0 {
		narrow = true
	}

	if !narrow {
		return screenLayout{Stack: false, ActionColumns: 0, ChatMinWidth: 320}
	}

	columns := 2
	if width > 0 && width < 300 {
		columns = 1
	}

	return screenLayout{Stack: true, ActionColumns: columns}
}
