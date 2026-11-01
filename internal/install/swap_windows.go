//go:build windows

package install

import (
	"errors"
	"fmt"
	"os"
)

// oldSuffix names the binary a Windows install moved aside. It is not the same
// file as PreviousSuffix: this one is a running executable that Windows keeps
// mapped until the process exits, so it cannot be deleted — only renamed away
// and cleaned up on the next run.
const oldSuffix = ".old"

// replace moves src over target, doing the dance Windows needs.
//
// Windows refuses to rename over a file that is mapped, and the binary doing
// the replacing is the one that is mapped. So the running executable is renamed
// aside first — that it allows — the new one takes its name, and the old one is
// put back if the second step fails. The aside file is deleted by the next
// install, because only then is it unmapped.
func replace(src, target string) error {
	aside := target + oldSuffix

	// A previous run that died between the two renames left the target missing
	// and the binary under the aside name. Put it back before trying again:
	// otherwise this install would report "no such file" about a binary that is
	// still there under another name.
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if _, asideErr := os.Stat(aside); asideErr == nil {
			if restoreErr := os.Rename(aside, target); restoreErr != nil {
				return fmt.Errorf("could not restore %s from %s: %w", target, aside, restoreErr)
			}
		}
	}

	// A leftover from the last run is deletable now, since that process is gone.
	if _, err := os.Stat(aside); err == nil {
		if err := os.Remove(aside); err != nil {
			return fmt.Errorf("could not clear the previous %s: %w", aside, err)
		}
	}

	if err := os.Rename(target, aside); err != nil {
		// No target to move aside is a first install; anything else is a real
		// failure, and the target is still where it was.
		if errors.Is(err, os.ErrNotExist) {
			return os.Rename(src, target)
		}

		return err
	}

	if err := os.Rename(src, target); err != nil {
		restoreErr := os.Rename(aside, target)
		if restoreErr != nil {
			return fmt.Errorf("could not place the new binary (%v) and could not put the old one back (%w)", err, restoreErr)
		}

		return err
	}

	// Best effort: the running binary is still mapped by this process until it
	// exits, so this usually fails and the next run clears it.
	_ = os.Remove(aside)

	return nil
}
