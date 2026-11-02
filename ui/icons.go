//go:build gui

package ui

import (
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// fileIconFor is the icon a file row shows for its kind.
//
// A row that draws nothing — a PDF, an audio clip, an archive — is otherwise a
// line of text with a filename, and a PDF and a song look exactly alike (#304).
// Fyne 2.8.2 has no PDF icon, so a PDF takes the generic document; a kind
// nothing else claims takes the application icon.
func fileIconFor(name string) fyne.Resource {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".m4a", ".aac", ".wav", ".ogg", ".oga", ".opus", ".flac", ".amr":
		return theme.FileAudioIcon()

	case ".mp4", ".m4v", ".mkv", ".mov", ".webm", ".avi", ".3gp":
		return theme.FileVideoIcon()

	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".heic":
		return theme.FileImageIcon()

	case ".txt", ".md", ".log", ".csv", ".json", ".yaml", ".yml", ".xml",
		".go", ".sh", ".bat", ".ps1", ".c", ".h", ".py", ".js", ".ts", ".html", ".css":
		return theme.FileTextIcon()
	}

	// The extension said nothing: fall back on the type, which may have come
	// from the bytes when the picker gave a name with no suffix (#303).
	switch {
	case strings.HasPrefix(mimeOf(name), "audio/"):
		return theme.FileAudioIcon()
	case strings.HasPrefix(mimeOf(name), "video/"):
		return theme.FileVideoIcon()
	case strings.HasPrefix(mimeOf(name), "image/"):
		return theme.FileImageIcon()
	case strings.HasPrefix(mimeOf(name), "text/"):
		return theme.FileTextIcon()
	}

	// A PDF has no icon of its own in this toolkit; the generic document is the
	// honest nearest thing, and it still says "a file you open", not "a picture".
	if strings.EqualFold(filepath.Ext(name), ".pdf") {
		return theme.FileIcon()
	}

	return theme.FileApplicationIcon()
}
