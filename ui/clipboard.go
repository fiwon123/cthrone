//go:build gui

package ui

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

// errNoImageClipboard is what a row says when the platform has no way to put a
// picture on the clipboard. Fyne's own clipboard is text, so this is a real
// possibility, not a defensive branch (#510).
var errNoImageClipboard = errors.New("this system has no image clipboard — use Save as… and copy the file from there")

// copyImageToOS puts the picture at path on the system clipboard as a picture.
//
// It is a variable so a test can watch what a row asks for without a clipboard.
// The work is done by the platform's own tool, in the build-tagged file beside
// this one: Windows has an API in the shell, macOS has one in AppleScript, and
// Linux has `wl-copy` or `xclip` when they are installed.
var copyImageToOS = copyImageToSystem

// imagePNG returns the picture at path as PNG bytes, which is what every
// platform's tool accepts.
//
// The file's own encoding is kept when it is already PNG; anything else is
// decoded and re-encoded, which is also the one place a copy can fail on a
// corrupt file.
func imagePNG(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if filepath.Ext(path) == ".png" || bytes.HasPrefix(raw, []byte("\x89PNG")) {
		return raw, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
