//go:build gui

package ui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	installpkg "github.com/fiwon123/cthrone/internal/install"
	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

// The seams the install path is driven through. They are variables for the
// reason the transports are: a test drives the same code against a temporary
// directory and an httptest server, so nothing here reaches github.com or
// replaces the test binary.
var (
	installBase = installpkg.GitHubAPI
	runInstall  = installpkg.Run
	runRevert   = installpkg.Revert
	selfBinary  = installpkg.RunningBinary

	// askConfirm is the question before anything is replaced. A test swaps it
	// for one that answers, or leaves it and asserts that nothing happened.
	askConfirm = func(parent fyne.Window, title, message string, onYes func()) {
		question := dialog.NewConfirm(title, message, func(confirmed bool) {
			if confirmed {
				onYes()
			}
		}, parent)
		question.Show()
	}
)

// offerInstall shows the Install button for a release the check named, or hides
// it when this build is already the newest one.
func (g *gui) offerInstall(tag string) {
	if g.installBtn == nil {
		return
	}

	if tag == "" {
		g.installBtn.Hide()

		return
	}

	g.installBtn.SetText("Install " + tag)
	g.installBtn.Show()
}

// installRelease replaces this binary with the release the check found, after
// asking. Nothing is replaced before the answer.
func (g *gui) installRelease() {
	tag := g.aboutOffer
	if tag == "" {
		return
	}

	askConfirm(g.win, "Install "+tag+"?",
		"This replaces the binary that is running with "+tag+" from the release page. "+
			"The download is checked against the release's checksums first, and the binary it replaces is kept "+
			"beside it, so it can be put back.",
		func() { g.runInstall(tag) })
}

// revertRelease puts the previous binary back, after asking. It needs no
// network: the binary it returns to is already beside this one.
func (g *gui) revertRelease() {
	askConfirm(g.win, "Go back to the previous binary?",
		"The binary this one replaced is put back, and the one it replaces is kept, so this can be undone the same way.",
		g.runRevert)
}

// runInstall does the install off the UI goroutine and writes the outcome on
// the About tab.
func (g *gui) runInstall(tag string) {
	if g.installBtn != nil {
		g.installBtn.Disable()
	}

	g.setAboutNote("Installing " + tag + "…")

	go func() {
		target, err := selfBinary()
		if err == nil {
			var result installpkg.Result

			ctx, cancel := context.WithTimeout(context.Background(), installpkg.InstallTimeout)
			defer cancel()

			result, err = runInstall(ctx, &http.Client{Timeout: installpkg.InstallTimeout}, installBase, installpkg.Request{
				Tag:      tag,
				GUIBuild: true,
				GOOS:     goos(),
				GOARCH:   goarch(),
				Target:   target,
			})
			if err == nil {
				err = noteInstalled(result)
			}
		}

		fyne.Do(func() {
			if g.installBtn != nil {
				g.installBtn.Enable()
			}

			if err != nil {
				g.setAboutNote("Could not install: " + err.Error() + ".")

				return
			}

			g.setAboutNote("Installed " + tag + ". Restart cthrone to run it.")
			g.refreshRevert()
		})
	}()
}

// runRevert puts the previous binary back, off the UI goroutine.
func (g *gui) runRevert() {
	g.setAboutNote("Going back…")

	go func() {
		target, err := selfBinary()
		if err == nil {
			_, err = runRevert(target)
		}

		fyne.Do(func() {
			if err != nil {
				g.setAboutNote("Could not go back: " + err.Error() + ".")

				return
			}

			g.setAboutNote("Put the previous binary back. Restart cthrone to run it.")
			g.refreshRevert()
		})
	}()
}

// refreshRevert shows the way back only when there is one: a button that can
// only answer "no previous binary" reads as a broken feature.
func (g *gui) refreshRevert() {
	if g.revertBtn == nil {
		return
	}

	target, err := selfBinary()
	if err != nil {
		g.revertBtn.Hide()

		return
	}

	if _, err := os.Stat(target + installpkg.PreviousSuffix); err != nil {
		g.revertBtn.Hide()

		return
	}

	g.revertBtn.Show()
}

// noteInstalled is the check a hand install would do: the file that was written
// is the file that is now running's path, and it is executable.
func noteInstalled(result installpkg.Result) error {
	info, err := os.Stat(result.Target)
	if err != nil {
		return fmt.Errorf("the binary is not where it should be: %w", err)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", result.Target)
	}

	return nil
}

// goos and goarch are the platform this build runs on. They are functions so
// the request the install makes is easy to read in a test.
func goos() string { return runtime.GOOS }

// goarch is the architecture this build runs on.
func goarch() string { return runtime.GOARCH }

// offerNewer shows the Install button only when the release the check found is
// newer than this build: a build that cannot read its own version is never
// told to replace itself, because the comparison would be a guess.
func (g *gui) offerNewer(current, tag string) {
	currentParts, ok := updatepkg.Parse(current)
	if !ok {
		g.aboutOffer = ""
		g.offerInstall("")

		return
	}

	latestParts, ok := updatepkg.Parse(tag)
	if !ok || updatepkg.Compare(currentParts, latestParts) >= 0 {
		g.aboutOffer = ""
		g.offerInstall("")

		return
	}

	g.aboutOffer = tag
	g.offerInstall(tag)
}
