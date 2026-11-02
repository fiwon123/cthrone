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
	readText      = textPreview
	canDecodeFile = decodableImage
)

// previewPicture is the decoded picture for path, decoded once.
//
// The list is virtualized, but painting a row is not free: before this cache, a
// visible picture row decoded the whole image on every repaint, and every
// incoming message repainted the visible rows. A transcript with a few pictures
// therefore grew slower with each message, which is what "slow after a while"
// was (#321).
func (g *gui) previewPicture(path string) fyne.Resource {
	if res, ok := g.pictures[path]; ok {
		return res
	}

	res, err := loadPicture(path)
	if err != nil {
		return nil
	}

	if g.pictures == nil {
		g.pictures = make(map[string]fyne.Resource)
	}

	if len(g.pictures) >= previewCacheMax {
		clear(g.pictures)
	}

	g.pictures[path] = res

	return res
}

// decodable is whether the picture at path can be decoded, asked once per path.
func (g *gui) decodable(path string) bool {
	if ok, known := g.decodableFiles[path]; known {
		return ok
	}

	ok := canDecodeFile(path)

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
	if body, ok := g.texts[path]; ok {
		return body
	}

	body := readText(path)

	if g.texts == nil {
		g.texts = make(map[string]string)
	}

	if len(g.texts) >= previewCacheMax {
		clear(g.texts)
	}

	g.texts[path] = body

	return body
}
