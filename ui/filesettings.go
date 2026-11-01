package ui

import (
	"errors"
	"fmt"
	"github.com/fiwon123/cthrone/internal/wire"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxSuffixes bounds the search for a free name. A folder with a thousand files
// called the same thing is a folder nobody can use; failing there is better than
// counting forever.
const maxSuffixes = 1000

// The preference keys. They are namespaced because the store is the toolkit's,
// shared with any other setting the window ever needs.
const (
	prefsDownloadFolder = "download.folder"
	prefsDownloadAsk    = "download.ask"
)

// savePolicy is what happens to a file when it has finished arriving.
type savePolicy int

const (
	// saveAuto writes the file into the download folder without asking. It is
	// the default, because a queue of files should not become a queue of
	// dialogs.
	saveAuto savePolicy = iota

	// saveAsk leaves the file in the staging folder until the user has chosen
	// where it goes. A transfer the user declines leaves nothing behind.
	saveAsk
)

// preferenceStore is the slice of fyne.Preferences the file settings use.
//
// It is an interface so the rules can be tested without a toolkit: the toolkit
// is what proves the widget, not what proves the policy.
type preferenceStore interface {
	String(key string) string
	Bool(key string) bool
	SetString(key, value string)
	SetBool(key string, value bool)
}

// fileSettings is where received files go, and whether the user is asked first.
type fileSettings struct {
	folder string
	policy savePolicy
}

// readFileSettings reads the preference store, falling back to a folder the
// platform suggests when nothing has been chosen yet.
//
// An unreadable folder is not remembered: a preference that names a path this
// machine does not have, because it was copied from another one, must not stop
// a file from being saved. The fallback is used instead, and the dialog shows
// which folder is really in force.
func readFileSettings(store preferenceStore, fallback string) fileSettings {
	settings := fileSettings{folder: fallback, policy: saveAuto}
	if store == nil {
		return settings
	}

	if folder := expandFolder(store.String(prefsDownloadFolder)); folder != "" {
		settings.folder = folder
	}

	if store.Bool(prefsDownloadAsk) {
		settings.policy = saveAsk
	}

	return settings
}

// writeFileSettings stores the choice, so it survives a restart.
func writeFileSettings(store preferenceStore, settings fileSettings) {
	if store == nil {
		return
	}

	store.SetString(prefsDownloadFolder, settings.folder)
	store.SetBool(prefsDownloadAsk, settings.policy == saveAsk)
}

// expandFolder turns what a user typed into a path.
//
// A leading "~" is expanded, because a folder typed by hand is typed the way a
// shell would read it, and a path that is not absolute is refused: the working
// directory of a GUI is wherever the launcher happened to put it, so a relative
// path would save a file somewhere nobody would look for it.
func expandFolder(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path[1:], "/"), `\`))
		}
	}

	if !filepath.IsAbs(path) {
		return ""
	}

	return filepath.Clean(path)
}

// numberedName is name for i == 0 and "stem (i).ext" after that. The rule is the
// assembler's (#112), kept identical so a name that arrives twice is resolved
// the same way whether it is the receiver or the move that notices.
func numberedName(name string, i int) string {
	if i == 0 {
		return name
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	return fmt.Sprintf("%s (%d)%s", stem, i, ext)
}

// uniquePath picks a name in dir that no file holds, without replacing one.
//
// ask mode settles a collision with the save dialog's own overwrite
// confirmation, because there overwriting is the user's explicit choice. Every
// other path goes through here: a file an earlier session left in the folder is
// never replaced silently.
func uniquePath(dir, name string) (string, error) {
	// The name arrives from the wire, where it has already been checked — and
	// it is checked again here, because this is the function that joins it onto
	// a path. A helper whose safety depends on a remote caller is one refactor
	// away from writing outside the folder it was given.
	if !wire.SafeName(name) {
		return "", fmt.Errorf("refusing to save a file as %q: not a plain name", name)
	}

	for i := 0; i <= maxSuffixes; i++ {
		candidate := filepath.Join(dir, numberedName(name, i))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no free name for %q in %s", name, dir)
}

// moveInto puts a staged file into dir under a name no other file holds, and
// reports where it landed.
//
// A rename is the fast path and keeps a 200 MB video from being copied twice.
// It fails when the destination is another filesystem — a folder on a stick,
// say — and then the bytes are copied and the staged copy is dropped.
func moveInto(source, dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	target, err := uniquePath(dir, name)
	if err != nil {
		return "", err
	}

	if err := os.Rename(source, target); err == nil {
		return target, nil
	}

	if err := copyFile(source, target); err != nil {
		os.Remove(target)

		return "", err
	}

	if err := os.Remove(source); err != nil {
		// The file is saved; the staged copy lingering is litter, not a
		// failure, and saying so would only worry the user.
		return target, nil
	}

	return target, nil
}

// copyInto writes a source file into w, the writer a save dialog hands back.
//
// The reader is the verified copy that already arrived, so a failed write is a
// failed copy and never a damaged file: the original stays where it was.
func copyInto(w io.Writer, source string) error {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	defer src.Close()

	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("writing %s: %w", source, err)
	}

	return nil
}

// copyFile copies source to target, which must not already exist.
func copyFile(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()

		return fmt.Errorf("writing %s: %w", target, err)
	}

	if err := dst.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}

	return nil
}

// defaultStagingDir is where a file is assembled before it is saved: inside the
// app's own storage, which exists on a phone and on a desktop alike.
//
// It is deliberately not the download folder. Ask mode must be able to decline a
// file, and a file that never reached the download folder is one there is
// nothing to undo.
func defaultStagingDir(storageRoot string) string {
	if storageRoot != "" {
		return filepath.Join(storageRoot, "incoming")
	}

	return filepath.Join(os.TempDir(), "cthrone", "incoming")
}
