//go:build gui && darwin

package ui

import (
	"fmt"
	"os"
	"os/exec"
)

// copyImageToSystem puts a picture on the macOS clipboard.
//
// AppleScript is the route that needs no cgo: `pbcopy` copies text, and the
// toolkit itself has no image clipboard (#510).
func copyImageToSystem(path string) error {
	picture, err := imagePNG(path)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "cthrone-picture-*.png")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)

	if _, err := file.Write(picture); err != nil {
		file.Close()

		return err
	}

	file.Close()

	script := fmt.Sprintf("set the clipboard to (read (POSIX file %q) as «class PNGf»)", name)

	return exec.Command("osascript", "-e", script).Run()
}
