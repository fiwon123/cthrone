// Command mobile is the entry point of the Android and iOS app.
//
// It is not part of the CLI: the root main.go stays the cobra command tree.
// This package exists so a mobile build has a plain `main` that starts the
// GUI — the APK target runs
//
//	cd mobile && fyne package -target android/arm64 -tags gui
//
// Without the `gui` tag it still compiles, against the stub in
// ui/app_nogui.go, which keeps `go build ./...` happy on every machine.
package main

import "github.com/fiwon123/cthrone/ui"

func main() {
	ui.Run()
}
