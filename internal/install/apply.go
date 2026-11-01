package install

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	updatepkg "github.com/fiwon123/cthrone/internal/update"
)

// InstallTimeout bounds a whole install: two requests and a rename. It is
// longer than the check's five seconds because an archive is a few megabytes
// over a link that may be slow, and a download cut short is worse than one that
// took a while.
const InstallTimeout = 2 * time.Minute

// Request is one install: which release, which build of it, and where to put
// the result.
type Request struct {
	// Tag names a published release, which is how an older version is chosen.
	// Empty asks for the newest one.
	Tag string

	// GUIBuild picks the window's archive rather than the command line's: they
	// are different files, and only one of them holds the binary that is
	// running.
	GUIBuild bool

	GOOS   string
	GOARCH string

	// Target is the executable to replace.
	Target string
}

// Result is what an install did: which release, which archive, where it landed,
// and where the binary it replaced is kept.
type Result struct {
	Tag      string
	Asset    string
	Target   string
	Previous string
	SHA256   string
}

// Run installs one release: fetch its document, name the archive for this
// build, verify the download against the release's own checksums, and swap it
// in.
//
// Nothing is replaced until the download has matched, and the swap is a rename
// of a fully written file, so a failure at any point leaves the running binary
// exactly where it was.
func Run(ctx context.Context, client *http.Client, apiBase string, request Request) (Result, error) {
	if request.Target == "" {
		return Result{}, errors.New("install: no target named")
	}

	url := apiBase + "/releases/latest"
	if request.Tag != "" {
		url = apiBase + "/releases/tags/" + request.Tag
	}

	release, err := Fetch(ctx, client, url)
	if err != nil {
		return Result{}, err
	}

	asset, ok := updatepkg.ArchiveName(release.Tag, request.GUIBuild, request.GOOS, request.GOARCH)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s publishes nothing for %s/%s", ErrNoAsset, release.Tag, request.GOOS, request.GOARCH)
	}

	return ApplyRelease(ctx, client, release, asset, Member(request.GUIBuild), request.Target)
}

// ApplyRelease installs one archive of a release that has already been fetched.
//
// It is separate from Run so a test can hand it a release it built, and so a
// caller that has already read the document does not read it twice.
func ApplyRelease(ctx context.Context, client *http.Client, release *Release, assetName, member, target string) (Result, error) {
	if assetName == "" || member == "" {
		return Result{}, errors.New("install: no archive or binary named")
	}

	archive, ok := release.Asset(assetName)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s is not in %s", ErrNoAsset, assetName, release.Tag)
	}

	checksums, ok := release.Asset(ChecksumsName)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s published no %s", ErrNoAsset, release.Tag, ChecksumsName)
	}

	sums, err := Download(ctx, client, checksums.URL, maxChecksums)
	if err != nil {
		return Result{}, err
	}

	body, err := Download(ctx, client, archive.URL, maxArchive)
	if err != nil {
		return Result{}, err
	}

	// Before anything on disk changes.
	if err := Verify(sums, assetName, body); err != nil {
		return Result{}, err
	}

	binary, err := Binary(body, assetName, member)
	if err != nil {
		return Result{}, err
	}

	previous, err := Install(target, binary)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Tag:      release.Tag,
		Asset:    assetName,
		Target:   target,
		Previous: previous,
		SHA256:   hex.EncodeToString(sum(binary)),
	}, nil
}

// Member is the binary to take out of an archive: the window build and the
// command line build are different files, and installing the wrong one over the
// running binary is the mistake with no undo.
func Member(gui bool) string {
	if gui {
		return "cthrone_gui"
	}

	return "cthrone"
}

// RunningBinary is the path of the executable doing the installing.
//
// A symlink is resolved: the file to replace is the one the link points at, not
// the link — renaming over a link would leave it pointing at the old binary.
func RunningBinary() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	return path, nil
}
