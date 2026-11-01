//go:build !windows

package install

import "os"

// replace moves src over target.
//
// On a POSIX system this is one call: rename(2) over an existing file is
// atomic, so a process reading the path — or a crash — sees either the old
// binary or the new one. There is no window in which the target is missing or
// half-written, which is what makes an interrupted install harmless.
func replace(src, target string) error {
	return os.Rename(src, target)
}
