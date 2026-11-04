//go:build gui

package ui

import (
	"fyne.io/fyne/v2"
)

// previewCacheMax bounds what the transcript remembers about the files it has
// painted. A conversation with more pictures than this is a conversation the
// user is scrolling through, and the oldest entries are cheap to rebuild.
const previewCacheMax = 64

// The seams a test uses to count how often a row does the work. They exist for
// the same reason the transport seams do: the defect is the *number of calls*,
// and a test that cannot count them cannot see it (#321).
var (
	loadPicture   = fyne.LoadResourceFromPath
	decodeThumb   = decodeThumbnailFile
	readText      = textPreview
	canDecodeFile = decodableImage
)

// previewPicture returns the thumbnail for path when it is ready, and starts
// decoding it when it is not.
//
// Decoding used to happen here, on the UI goroutine, at full size: a photo is
// tens of milliseconds of work and Fyne asks for a row's height while scrolling,
// so the window stalled on the picture it was scrolling onto (#510). Now the
// first paint says "not ready", one worker per path decodes a thumbnail, and the
// transcript repaints when it lands. ready=false means "draw the note, not the
// picture".
func (g *gui) previewPicture(path string) (fyne.Resource, bool) {
	g.cacheMu.Lock()
	if res, ok := g.pictures[path]; ok {
		g.touchPicture(path)
		g.cacheMu.Unlock()

		return res, true
	}

	if g.picturePending[path] {
		g.cacheMu.Unlock()

		return nil, false
	}

	if g.picturePending == nil {
		g.picturePending = make(map[string]bool)
	}

	g.picturePending[path] = true
	g.cacheMu.Unlock()

	// A test paints a row and expects the picture in the same call: the worker
	// is the point of this function in a window, and noise in a test about what
	// a row draws (#510).
	if inlinePictureDecode {
		return g.decodeNow(path)
	}

	go g.decodePreview(path)

	return nil, false
}

// inlinePictureDecode makes previewPicture decode on the calling goroutine. It
// is set by tests that paint a row once and look at it, and never by the window.
var inlinePictureDecode = false

// decodeNow is the body of the worker, run in the caller.
func (g *gui) decodeNow(path string) (fyne.Resource, bool) {
	res, err := decodeThumbnail(path)

	g.cacheMu.Lock()
	delete(g.picturePending, path)

	if err == nil {
		g.putPicture(path, res)
	}
	g.cacheMu.Unlock()

	if err != nil {
		return nil, false
	}

	return res, true
}

// thumbnailMax is the longest edge a transcript thumbnail keeps. The file on
// disk is untouched: this only bounds what a row holds in memory, so a
// conversation of photos does not cost a conversation of full-size images.
const thumbnailMax = 512

// decodePreview decodes one path into a thumbnail and puts it in the cache.
func (g *gui) decodePreview(path string) {
	res, err := decodeThumbnail(path)

	g.cacheMu.Lock()
	delete(g.picturePending, path)

	if err == nil {
		g.putPicture(path, res)
	}
	g.cacheMu.Unlock()

	if err != nil {
		return
	}

	// One repaint for the whole transcript: the rows that were waiting are the
	// reason, and a row that has scrolled away simply paints again.
	fyne.Do(g.repaintPictures)
}

// putPicture stores a thumbnail, dropping the least recently used one when the
// cache is full. It is called with cacheMu held.
func (g *gui) putPicture(path string, res fyne.Resource) {
	if g.pictures == nil {
		g.pictures = make(map[string]fyne.Resource)
	}

	for len(g.pictureOrder) >= previewCacheMax {
		oldest := g.pictureOrder[0]
		g.pictureOrder = g.pictureOrder[1:]
		delete(g.pictures, oldest)
	}

	g.pictures[path] = res
	g.pictureOrder = append(g.pictureOrder, path)
}

// touchPicture marks a path as the most recently used. It is called with
// cacheMu held.
func (g *gui) touchPicture(path string) {
	for i, known := range g.pictureOrder {
		if known != path {
			continue
		}

		g.pictureOrder = append(g.pictureOrder[:i], g.pictureOrder[i+1:]...)
		g.pictureOrder = append(g.pictureOrder, path)

		return
	}
}

// repaintPictures asks for the transcript to be painted again once a thumbnail
// has landed.
func (g *gui) repaintPictures() {
	if g.chatList == nil {
		return
	}

	g.chatList.Refresh()
}

// decodable is whether the picture at path can be decoded, asked once per path.
func (g *gui) decodable(path string) bool {
	g.cacheMu.Lock()
	ok, known := g.decodableFiles[path]
	g.cacheMu.Unlock()

	if known {
		return ok
	}

	ok = canDecodeFile(path)

	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()

	if g.decodableFiles == nil {
		g.decodableFiles = make(map[string]bool)
	}

	if len(g.decodableFiles) >= previewCacheMax {
		clear(g.decodableFiles)
	}

	g.decodableFiles[path] = ok

	return ok
}

// previewText is the bounded text of a text file, read once.
func (g *gui) previewText(path string) string {
	g.cacheMu.Lock()
	body, ok := g.texts[path]
	g.cacheMu.Unlock()

	if ok {
		return body
	}

	body = readText(path)

	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()

	if g.texts == nil {
		g.texts = make(map[string]string)
	}

	if len(g.texts) >= previewCacheMax {
		clear(g.texts)
	}

	g.texts[path] = body

	return body
}
