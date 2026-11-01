// Package install replaces the running binary with one from a published
// release, and puts the previous one back.
//
// It exists because "a newer version exists" is half an answer: the user still
// has to find the archive for their platform, unpack it, and replace the binary
// by hand — and a release that misbehaves has no way back. The two halves are
// one path here: fetch the release the check named (or any published older
// one), verify the download against the release's own checksums.txt, unpack the
// single binary, and swap it in while keeping what it replaced.
//
// The rules are the ones the update check follows: one public document over
// HTTPS and no credential, and every network call a parameter so a test can
// point it at an httptest server. Nothing here is silent: the caller asks, and
// the package reports what it did — the release, the hash it matched, the file
// it replaced, and the file to go back to.
package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// ChecksumsName is the file the release publishes beside its archives: one
	// `sha256sum` line per asset.
	ChecksumsName = "checksums.txt"

	// maxArchive bounds a download. The largest asset in a release is the APK
	// at a few tens of megabytes; a response far past that is not one this
	// program should hold in memory.
	maxArchive = 256 << 20

	// maxChecksums bounds the checksums file, which is a few hundred bytes.
	maxChecksums = 1 << 20
)

// ErrChecksum is reported when a download does not match the release's own
// record of it. It is a refusal, not a warning: the bytes are not the ones the
// project published.
var ErrChecksum = errors.New("install: the download does not match the release's checksum")

// ErrNoAsset is reported when a release does not publish the file this build
// would install.
var ErrNoAsset = errors.New("install: the release publishes no archive for this platform")

// Asset is one file a release published.
type Asset struct {
	Name string
	URL  string
	Size int64
}

// Release is the part of a published release this package needs.
type Release struct {
	Tag    string
	Assets []Asset
}

// Asset finds a published file by name.
func (r *Release) Asset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}

	return Asset{}, false
}

// GitHubAPI is the base every release document is read from. It is a constant
// because a release is a public document: the check and the install must not
// need a credential, and a mirror is not a thing this program supports.
const GitHubAPI = "https://api.github.com/repos/fiwon123/cthrone"

// LatestURL names the newest published release.
func LatestURL() string { return GitHubAPI + "/releases/latest" }

// TagURL names one published release, which is how an older version is asked
// for by name.
func TagURL(tag string) string { return GitHubAPI + "/releases/tags/" + tag }

// Fetch reads a release document: the tag, and the files it published.
//
// The URL is a parameter so a test can serve a release it built itself; a test
// that reached github.com would fail when the network does and prove nothing
// when it passed.
func Fetch(ctx context.Context, client *http.Client, url string) (*Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the release list answered %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxChecksums))
	if err != nil {
		return nil, err
	}

	var document struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("the release did not read as JSON: %w", err)
	}

	tag := strings.TrimSpace(document.TagName)
	if tag == "" {
		return nil, errors.New("the release named no version")
	}

	release := &Release{Tag: tag}
	for _, asset := range document.Assets {
		release.Assets = append(release.Assets, Asset{Name: asset.Name, URL: asset.URL, Size: asset.Size})
	}

	return release, nil
}

// Download fetches one file.
//
// limit is the most it will read: a bounds check on a response whose size the
// caller did not choose. It is a parameter rather than a constant so the
// checksums file and an archive can share this function with different ceilings.
func Download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the download answered %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("the download is larger than %d bytes", limit)
	}

	return body, nil
}

// Verify checks a download against the release's checksums.txt.
//
// The file is the one the release workflow writes — `sha256sum` output, a hash
// and a name per line — so this is the same check a user would run by hand,
// made before anything is replaced rather than after.
func Verify(checksums []byte, name string, archive []byte) error {
	want, ok := ChecksumFor(checksums, name)
	if !ok {
		return fmt.Errorf("%w: %s is not listed in %s", ErrChecksum, name, ChecksumsName)
	}

	got := hex.EncodeToString(sum(archive))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w: %s is %s, the release says %s", ErrChecksum, name, got, want)
	}

	return nil
}

// ChecksumFor reads the hash a checksums.txt gives for one file.
func ChecksumFor(checksums []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(checksums), "\n") {
		// `sha256sum` writes "<hash>  <name>", and some tools use a single
		// space or a "*" for binary mode. Split on any run of them.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		if strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], true
		}
	}

	return "", false
}

// Binary unzips the named member out of an archive, deciding the format by the
// archive's own name: the CLI and window archives are `.tar.gz` everywhere but
// Windows, which publishes `.zip`.
func Binary(archive []byte, archiveName, member string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return fromZip(archive, member)
	}

	return fromTarGz(archive, member)
}

func fromTarGz(archive []byte, member string) ([]byte, error) {
	gzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive did not read as gzip: %w", err)
	}
	defer gzipped.Close()

	reader := tar.NewReader(gzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("the archive did not read as tar: %w", err)
		}

		// The archives are flat, but a leading "./" is what some tar writers
		// produce, and it is not a different file.
		if strings.TrimPrefix(header.Name, "./") == member {
			return io.ReadAll(reader)
		}
	}

	return nil, fmt.Errorf("the archive holds no %s", member)
}

func fromZip(archive []byte, member string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("the archive did not read as zip: %w", err)
	}

	for _, file := range reader.File {
		if strings.TrimPrefix(file.Name, "./") != member {
			continue
		}

		opened, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer opened.Close()

		return io.ReadAll(opened)
	}

	return nil, fmt.Errorf("the archive holds no %s", member)
}

// sum is the sha256 of the bytes, as the checksums file spells it.
func sum(data []byte) []byte {
	total := sha256.Sum256(data)

	return total[:]
}
