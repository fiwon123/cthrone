//go:build gui

package ui

import (
	"path/filepath"

	"fyne.io/fyne/v2"
)

// decodeThumbnail is the seam: tests replace decodeThumb to control what a
// thumbnail is and how often it is made (#510).
func decodeThumbnail(path string) (fyne.Resource, error) {
	return decodeThumb(path)
}

// decodeThumbnailFile makes the resource a picture row draws.
//
// A thumbnail, not the picture: the row is a couple of hundred pixels tall, and
// a full-size decode is what made scrolling stall (#510). thumbnailPNG already
// averages from the source — it is the same function the sender's row uses for a
// picker that left no file behind (#303).
//
// A picture too large for that helper falls back to the whole file: a slow paint
// is better than a row that never shows anything.
func decodeThumbnailFile(path string) (fyne.Resource, error) {
	if small := thumbnailPNG(path, thumbnailMax); small != nil {
		return fyne.NewStaticResource(filepath.Base(path)+".png", small), nil
	}

	return loadPicture(path)
}
