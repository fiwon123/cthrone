package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PreviousSuffix names the copy of the binary an install replaced. It sits
// beside the binary rather than in a cache directory: a download that vanished
// is a download you fetch again, a previous version is the thing you reach for
// when the new one will not start.
const PreviousSuffix = ".previous"

// ErrNoPrevious is reported when there is nothing to go back to.
var ErrNoPrevious = errors.New("install: no previous binary to revert to")

// Install replaces target with content, and keeps the binary it replaced beside
// it as "<target>.previous".
//
// The order is the point. The new file is written and synced first, under a
// temporary name in the same directory; the current binary is copied aside
// next; and only then is the new one moved into place. A failure at any step
// leaves the target exactly as it was, and nothing half-written is ever
// published, because the target is only ever replaced by a rename of a
// complete file.
//
// What a crash leaves behind depends on the platform, and that lives in
// replace(): on Linux and macOS the rename is atomic, so the target is always
// one of the two binaries; on Windows the running executable cannot be replaced,
// so it is renamed aside first (see swap_windows.go) and the next install
// restores it if the process died in between.
//
// It returns the path of the copy the way back uses.
func Install(target string, content []byte) (string, error) {
	if len(content) == 0 {
		return "", errors.New("install: the new binary is empty")
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}

	previous := target + PreviousSuffix

	staged, err := writeStaged(target, content)
	if err != nil {
		return "", err
	}

	// The staged file is removed on every path that does not rename it into
	// place: a leftover next to the binary would be picked up by nothing and
	// confuse a user looking at their install directory.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(staged)
		}
	}()

	// Nothing to keep on a first install: the copy is of the binary this
	// replaces, and there is none.
	if _, err := os.Stat(target); err == nil {
		if err := copyAside(target, previous); err != nil {
			return "", err
		}
	}

	if err := replace(staged, target); err != nil {
		return "", err
	}

	renamed = true

	return previous, nil
}

// Revert puts the previous binary back, without a network call.
//
// It is Install with the previous bytes, which is what makes it a toggle:
// Install copies what it replaces aside, so reverting once gives the way back
// to the version it replaced, and reverting again returns to where you started.
func Revert(target string) (string, error) {
	previous := target + PreviousSuffix

	content, err := os.ReadFile(previous)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s is missing", ErrNoPrevious, previous)
	}

	if err != nil {
		return "", err
	}

	if _, err := Install(target, content); err != nil {
		return "", err
	}

	return previous, nil
}

// writeStaged writes content to a new file beside target, executable, and
// synced: a rename can publish a fully written file, not a buffered one.
//
// The mode is the target's own when there is one, so an install does not
// quietly change the permissions of a binary the user or their package manager
// set.
func writeStaged(target string, content []byte) (string, error) {
	mode := os.FileMode(0o755)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	file, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".new-*")
	if err != nil {
		return "", err
	}

	name := file.Name()

	cleanup := func(err error) (string, error) {
		file.Close()
		_ = os.Remove(name)

		return "", err
	}

	if _, err := file.Write(content); err != nil {
		return cleanup(err)
	}

	if err := file.Chmod(mode); err != nil {
		return cleanup(err)
	}

	if err := file.Sync(); err != nil {
		return cleanup(err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(name)

		return "", err
	}

	return name, nil
}

// copyAside writes the current binary to the name the way back uses.
//
// It goes through a temporary file in the same way the install does: a crash
// halfway through a copy must not leave a truncated "previous" binary, because
// that is the one the user reaches for when the new version fails.
func copyAside(target, previous string) error {
	source, err := os.Open(target)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}

	staged, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(previous)+".new-*")
	if err != nil {
		return err
	}

	name := staged.Name()

	cleanup := func(err error) error {
		staged.Close()
		_ = os.Remove(name)

		return err
	}

	if _, err := io.Copy(staged, source); err != nil {
		return cleanup(err)
	}

	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		return cleanup(err)
	}

	if err := staged.Sync(); err != nil {
		return cleanup(err)
	}

	if err := staged.Close(); err != nil {
		_ = os.Remove(name)

		return err
	}

	if err := replace(name, previous); err != nil {
		_ = os.Remove(name)

		return err
	}

	return nil
}
