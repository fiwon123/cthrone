package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// androidManifest is the rendered manifest: the template with the release's
// version substituted, which is what `make gui-android` puts beside the package.
func androidManifest(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile("AndroidManifest.xml.tmpl")
	if err != nil {
		t.Fatalf("read the manifest template: %v", err)
	}

	rendered := strings.NewReplacer("@VERSION@", "9.9.9", "@BUILD@", "42").Replace(string(raw))

	for _, placeholder := range []string{"@VERSION@", "@BUILD@"} {
		if strings.Contains(rendered, placeholder) {
			t.Errorf("the template still holds %s after substitution", placeholder)
		}
	}

	return rendered
}

// manifestFacts is what the packager reads, and the one thing this app adds.
type manifestFacts struct {
	pkg         string
	versionName string
	versionCode string
	libName     string
	activity    string
	orientation string
}

func parseManifest(t *testing.T, document string) manifestFacts {
	t.Helper()

	var facts manifestFacts
	var inActivity bool

	decoder := xml.NewDecoder(strings.NewReader(document))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			if end, ok := token.(xml.EndElement); ok && end.Name.Local == "activity" {
				inActivity = false
			}

			continue
		}

		attr := func(name string) string {
			for _, a := range start.Attr {
				if a.Name.Local == name {
					return a.Value
				}
			}

			return ""
		}

		switch start.Name.Local {
		case "manifest":
			facts.pkg = attr("package")
			facts.versionName = attr("versionName")
			facts.versionCode = attr("versionCode")

		case "activity":
			inActivity = true
			facts.activity = attr("name")
			facts.orientation = attr("screenOrientation")

		case "meta-data":
			if inActivity && attr("name") == "android.app.lib_name" {
				facts.libName = attr("value")
			}
		}
	}

	return facts
}

// The packager reads a manifest from the package directory instead of generating
// one, and it refuses the build unless the activity is Go's and the library name
// is present. It also does not template a provided manifest, so the version has
// to be substituted by the Makefile — this is the check that both facts survive
// editing (#302).
func TestTheAndroidManifestKeepsWhatThePackagerNeeds(t *testing.T) {
	facts := parseManifest(t, androidManifest(t))

	if facts.activity != "org.golang.app.GoNativeActivity" {
		t.Errorf("activity = %q, want org.golang.app.GoNativeActivity", facts.activity)
	}

	if facts.libName != "cthrone" {
		t.Errorf("android.app.lib_name = %q, want cthrone", facts.libName)
	}

	if facts.versionName != "9.9.9" || facts.versionCode != "42" {
		t.Errorf("version %q/%q, want the substituted 9.9.9/42", facts.versionName, facts.versionCode)
	}
}

// The app stays upright: that is the whole point of shipping a manifest (#302).
func TestTheAndroidManifestLocksTheAppToPortrait(t *testing.T) {
	if got := parseManifest(t, androidManifest(t)).orientation; got != "portrait" {
		t.Errorf("screenOrientation = %q, want portrait", got)
	}
}

// The package name in the manifest and the one the Makefile passes as -app-id
// have to be the same, or the APK is installed as a different app from the one
// the build names.
func TestTheManifestPackageMatchesTheAppsID(t *testing.T) {
	facts := parseManifest(t, androidManifest(t))

	makefile, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}

	want := ""
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.Contains(line, "-app-id ") {
			want = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-app-id"))
			// The recipe line continues with a backslash.
			want = strings.TrimSpace(strings.TrimSuffix(want, "\\"))
			break
		}
	}

	if want == "" {
		t.Fatal("the Makefile no longer names an -app-id")
	}

	if facts.pkg != want {
		t.Errorf("manifest package = %q, Makefile -app-id = %q", facts.pkg, want)
	}
}

// The Android build number comes from the version, not from the commit count: CI
// checks out shallow, where the count is 1, and a versionCode that never changes
// makes an update indistinguishable from a reinstall (#315).
func TestTheBuildNumberComesFromTheVersion(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}

	recipe := string(makefile)

	if strings.Contains(recipe, "rev-list --count") {
		t.Error("the Android build number is taken from git history, which a shallow CI checkout reports as 1")
	}

	if !strings.Contains(recipe, "10000") {
		t.Error("the Android build number is not derived from the version")
	}
}
