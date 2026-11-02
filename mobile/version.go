//go:build !versionfile

package main

// buildVersion is the release this APK was packaged from, and it is empty in a
// plain `go build` or `go test`.
//
// `make gui-android` writes mobile/version_build.go with the tag and packages
// with `-tags versionfile`, so the About tab reports the same version every other
// archive does. Without it the APK fell back to the packager's metadata, which is
// not the tag (#343).
var buildVersion = ""
