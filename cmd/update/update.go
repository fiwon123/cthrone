// Package updatecmd is `cthrone update`: it asks whether a newer release exists,
// replaces this binary with one from a published release, and puts the previous
// one back.
//
// The check and the window's About tab are the same implementation
// (internal/update); the install and its undo are too (internal/install), so the
// terminal and the window cannot install different things.
//
// Nothing happens without being asked for: no flag means no request, and the
// command says what it can do instead of guessing.
package updatecmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"

	"github.com/spf13/cobra"

	installpkg "github.com/fiwon123/cthrone/internal/install"
	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

var (
	check     bool
	doInstall bool
	doRevert  bool
	toTag     string
)

// Version is the release this build came from, as `--version` reports it.
//
// It is a function rather than a copy of cmd.Version because a subpackage
// cannot import its parent: the root command points this at the variable the
// linker fills, so a release build reports its tag and a local one reports the
// vx.x.x placeholder that the check refuses to compare.
var Version = func() string { return "vx.x.x" }

// The seams a test needs. They are variables for the reason the transports are:
// a test points the real client at an httptest server and the real install at a
// temporary directory, because a check that reached github.com would fail when
// the network does and prove nothing when it passed.
var (
	releaseURL = updatepkg.LatestReleaseURL
	apiBase    = installpkg.GitHubAPI
	client     = func() *http.Client { return &http.Client{Timeout: installpkg.InstallTimeout} }
	runInstall = installpkg.Run
	runRevert  = installpkg.Revert
	selfBinary = installpkg.RunningBinary
)

// Cmd is the `update` command.
var Cmd = &cobra.Command{
	Use:   "update",
	Short: "check for, install and revert a release",
	Long: `Check whether a newer release of cthrone has been published, install one, or
go back to the previous binary.

--check reads the public release list over HTTPS and prints what it found. It
exits 0 whether or not a newer release exists — "up to date" is an answer, not a
failure — and non-zero only when the check could not be made.

--install replaces this binary with the one from a published release: the newest,
or the one named with --to. The download is verified against that release's
checksums.txt before anything is replaced, and the binary it replaces is kept
beside it, so --revert puts it back without a network call.

Only the binary that is running is replaced. The other files in the archive —
the ct alias, the window build — are left alone; unpack the archive for those.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd.Context(), cmd.OutOrStdout())
	},
}

func init() {
	Cmd.Flags().BoolVar(&check, "check", false, "ask the release list whether a newer version exists")
	Cmd.Flags().BoolVar(&doInstall, "install", false, "replace this binary with one from a published release")
	Cmd.Flags().BoolVar(&doRevert, "revert", false, "put the previous binary back, without a network call")
	Cmd.Flags().StringVar(&toTag, "to", "", "the release to install with --install (default: the newest)")
}

// run is the command without cobra around it, so a test can drive it.
func run(ctx context.Context, out io.Writer) error {
	if err := actionsAgree(); err != nil {
		return err
	}

	switch {
	case doRevert:
		return revert(out)
	case doInstall:
		return install(ctx, out)
	case check:
		return checkRelease(ctx, out)
	}

	fmt.Fprintln(out, "cthrone update --check reports whether a newer release exists.")
	fmt.Fprintln(out, "cthrone update --install [--to <tag>] replaces this binary with one from a published release.")
	fmt.Fprintln(out, "cthrone update --revert puts the previous binary back.")

	return nil
}

// actionsAgree refuses a command line that asks for two things at once: an
// install and a revert in one breath has no order the user could have meant.
func actionsAgree() error {
	named := 0
	for _, on := range []bool{check, doInstall, doRevert} {
		if on {
			named++
		}
	}

	if named > 1 {
		return fmt.Errorf("--check, --install and --revert do different things; name one")
	}

	if toTag != "" && !doInstall {
		return fmt.Errorf("--to names a release to install, so it needs --install")
	}

	return nil
}

// checkRelease prints whether a newer release exists.
func checkRelease(ctx context.Context, out io.Writer) error {
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

// install replaces this binary with one from a published release.
func install(ctx context.Context, out io.Writer) error {
	target, err := selfBinary()
	if err != nil {
		return fmt.Errorf("could not find the binary to replace: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, installpkg.InstallTimeout)
	defer cancel()

	result, err := runInstall(ctx, client(), apiBase, installpkg.Request{
		Tag:    toTag,
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		Target: target,
	})
	if err != nil {
		return fmt.Errorf("could not install: %w", err)
	}

	// Said before the restart, because the release this replaces may be the one
	// that cannot start.
	fmt.Fprintf(out, "Installed %s from %s.\n", result.Tag, result.Asset)
	fmt.Fprintf(out, "Verified %s against the release's checksums.txt.\n", result.Checksum)
	fmt.Fprintf(out, "%s replaced; the previous binary is kept at %s.\n", result.Target, result.Previous)
	fmt.Fprintln(out, "Restart cthrone to run it. `cthrone update --revert` puts the previous one back.")

	return nil
}

// revert puts the previous binary back.
func revert(out io.Writer) error {
	target, err := selfBinary()
	if err != nil {
		return fmt.Errorf("could not find the binary to revert: %w", err)
	}

	previous, err := runRevert(target)
	if err != nil {
		return fmt.Errorf("could not revert: %w", err)
	}

	fmt.Fprintf(out, "Put the previous binary back at %s.\n", target)
	fmt.Fprintf(out, "The one it replaced is kept at %s, so --revert again returns to it.\n", previous)
	fmt.Fprintln(out, "Restart cthrone to run it.")

	return nil
}
