//go:build gui && !windows && !darwin && (!linux || android)

package ui

// copyImageToSystem reports that this platform has no image clipboard.
//
// It covers Android and every other platform this GUI builds for. A phone would
// need Java glue in the APK's own template — the ClipboardManager is not
// reachable from Go — and that is its own piece of work (#206-style), not a
// branch here (#510).
func copyImageToSystem(string) error {
	return errNoImageClipboard
}
