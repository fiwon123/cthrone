//go:build gui && linux && !android

package ui

import (
	"bytes"
	"os/exec"
)

// copyImageToSystem puts a picture on the X11 or Wayland clipboard.
//
// Linux has no clipboard API in Go that does images, so the platform's own
// helper does it: `wl-copy` on Wayland, `xclip` on X11. Neither is installed
// everywhere, and the answer when neither is present is the sentence a row
// shows rather than a button that does nothing (#510).
func copyImageToSystem(path string) error {
	picture, err := imagePNG(path)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd := exec.Command("wl-copy", "--type", "image/png")
		cmd.Stdin = bytes.NewReader(picture)

		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-i")
		cmd.Stdin = bytes.NewReader(picture)

		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return errNoImageClipboard
}
