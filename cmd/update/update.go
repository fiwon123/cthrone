// Package updatecmd is `cthrone update`: it asks whether a newer release exists
// and reports the answer, the same answer the window shows on its About tab.
//
// It installs nothing. Replacing a binary — and getting back to the previous
// one — is #214; this command only reads one public document and says what it
// found, so a user on a build with a known defect can learn that a fixed
// release exists without leaving the terminal.
package updatecmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"

	"github.com/spf13/cobra"

	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

var check bool

// Version is the release this build came from, as `--version` reports it.
//
// It is a function rather than a copy of cmd.Version because a subpackage
// cannot import its parent: the root command points this at the variable the
// linker fills, so a release build reports its tag and a local one reports the
// vx.x.x placeholder that the check refuses to compare.
var Version = func() string { return "vx.x.x" }

// releaseURL is where the check reads. It is a variable for the reason the
// transports are: a test points the real client at an httptest server, because
// a check that reached github.com would fail when the network does and prove
// nothing when it passed.
var releaseURL = updatepkg.LatestReleaseURL

// Cmd is the `update` command.
var Cmd = &cobra.Command{
	Use:   "update",
	Short: "check whether a newer release exists",
	Long: `Check whether a newer release of cthrone has been published.

The check reads the public release list over HTTPS, prints what it found and
exits 0 whether or not a newer release exists — "up to date" is an answer, not a
failure. It exits non-zero only when the check itself could not be made.

Nothing is downloaded or installed.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd.Context(), cmd.OutOrStdout())
	},
}

func init() {
	Cmd.Flags().BoolVar(&check, "check", false, "ask the release list whether a newer version exists")
}

// run is the command without cobra around it, so a test can drive it.
func run(ctx context.Context, out io.Writer) error {
	if !check {
		// The command has one job today. Saying so is better than a silent exit
		// a user reads as "no update": the install path is #214 and does not
		// exist yet.
		fmt.Fprintln(out, "cthrone update --check reports whether a newer release exists.")
		fmt.Fprintln(out, "Installing one — or reverting to an older one — is not implemented yet (#214); the release page has the archives.")

		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, updatepkg.CheckTimeout)
	defer cancel()

	tag, err := updatepkg.LatestTag(ctx, &http.Client{Timeout: updatepkg.CheckTimeout}, releaseURL)
	if err != nil {
		// A failed check is the one thing that exits non-zero: the user asked a
		// question and did not get an answer.
		return fmt.Errorf("could not check: %w", err)
	}

	// The command line and the window are different files on every desktop
	// platform, so this names the command line's archive.
	archive, _ := updatepkg.ArchiveName(tag, false, runtime.GOOS, runtime.GOARCH)

	fmt.Fprintln(out, updatepkg.Text(Version(), tag, archive))

	return nil
}
