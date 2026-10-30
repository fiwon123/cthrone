//go:build !windows

package ui

func notifyFailure(err error) {
	// No-op on non-Windows platforms.
}
