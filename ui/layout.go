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
	// A phone stays stacked whatever the width. The window's orientation is asked
	// for as portrait (mobile/AndroidManifest.xml.tmpl), but Android 12L and later
	// ignore that on large screens for apps targeting older APIs, and a device
	// that rotates would otherwise bring the two-column desktop layout back onto a
	// handset (#302). The layout is the part this program can guarantee.
	narrow := mobile || (width > 0 && width < narrowWidth)

	if !narrow {
		return screenLayout{Stack: false, ChatMinWidth: 320}
	}

	return screenLayout{Stack: true}
}
