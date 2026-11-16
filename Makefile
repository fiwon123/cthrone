APP_NAME := cthrone
APP_NAME_SHORT := ct
BUILD_DIR := build
SCRIPTS_DIR := scripts
ASSETS_DIR := assets
SCRIPT_NAME := add_to_path
INJECT_VERSION:= github.com/fiwon123/cthrone/cmd.Version

# The GUI reports its own version on the About tab, so a window has to know the
# release it came from without a terminal to ask. The APK is the exception: it
# is packaged by `fyne package -app-version`, and the About tab reads that
# metadata when this line is not in the binary.
INJECT_GUI_VERSION:= github.com/fiwon123/cthrone/ui.Version

# Detect last tag and increment patch
VERSION := $(shell git describe --tags --always)

WINDOWS_BUILD:= $(BUILD_DIR)/Windows
WINDOWS_BIN := $(WINDOWS_BUILD)/$(APP_NAME).exe
SHORT_WINDOWS_BIN := $(WINDOWS_BUILD)/$(APP_NAME_SHORT).exe
WINDOWS_ZIP := $(WINDOWS_BUILD)/$(APP_NAME)_$(VERSION)_windows.zip
WINDOWS_SCRIPT :=  $(SCRIPTS_DIR)/$(SCRIPT_NAME).bat

LINUX_BUILD:= $(BUILD_DIR)/Linux
LINUX_BIN := $(LINUX_BUILD)/$(APP_NAME)
SHORT_LINUX_BIN := $(LINUX_BUILD)/$(APP_NAME_SHORT)
LINUX_TAR := $(LINUX_BUILD)/$(APP_NAME)_$(VERSION)_linux.tar.gz
LINUX_SCRIPT :=  $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh

ANDROID_BUILD:= $(BUILD_DIR)/Android
ANDROID_BIN := $(ANDROID_BUILD)/$(APP_NAME)
SHORT_ANDROID_BIN := $(ANDROID_BUILD)/$(APP_NAME_SHORT)
ANDROID_ZIP := $(ANDROID_BUILD)/$(APP_NAME)_$(VERSION)_android.zip
ANDROID_TAR := $(ANDROID_BUILD)/$(APP_NAME)_$(VERSION)_android.tar.gz

# macOS CLI. One archive per architecture: the two `cthrone` binaries cannot
# share a directory, so they cannot share an archive.
DARWIN_BUILD := $(BUILD_DIR)/Darwin
DARWIN_ARM64_DIR := $(DARWIN_BUILD)/arm64
DARWIN_AMD64_DIR := $(DARWIN_BUILD)/amd64
DARWIN_ARM64_TAR := $(DARWIN_BUILD)/$(APP_NAME)_$(VERSION)_darwin_arm64.tar.gz
DARWIN_AMD64_TAR := $(DARWIN_BUILD)/$(APP_NAME)_$(VERSION)_darwin_amd64.tar.gz

# The Fyne GUI (`-tags gui`). These are separate targets because they need
# CGO and platform headers a plain cross-compile does not have: GTK/X11/GL on
# Linux, mingw-w64 for Windows, and a macOS host for darwin. The local dev
# sandbox has none of them, which is why `release` does not depend on them.
GUI_BUILD := $(BUILD_DIR)/GUI
GUI_LINUX_DIR := $(GUI_BUILD)/Linux
GUI_WINDOWS_DIR := $(GUI_BUILD)/Windows
GUI_DARWIN_DIR := $(GUI_BUILD)/Darwin
GUI_LINUX_TAR := $(GUI_BUILD)/$(APP_NAME)_$(VERSION)_linux_gui_amd64.tar.gz
GUI_WINDOWS_ZIP := $(GUI_BUILD)/$(APP_NAME)_$(VERSION)_windows_gui_amd64.zip
GUI_DARWIN_TAR := $(GUI_BUILD)/$(APP_NAME)_$(VERSION)_darwin_gui_arm64.tar.gz
GUI_ANDROID_APK := $(GUI_BUILD)/$(APP_NAME)_$(VERSION)_android_gui_arm64.apk

# The gui-tagged widget tree is tested headless, which is the one GUI task that
# needs none of those headers: Fyne's `ci` tag swaps the GL driver for the
# software one, so this runs on any machine with Go. CI runs the same command,
# so a green `make check` and a green CI step mean the same thing.
GUI_TEST := CGO_ENABLED=0 go test -tags "gui ci" ./ui/... ./cmd/ui/...

# The fyne CLI that packages the APK. Pinned: the packager shapes the Android
# manifest and the signature, so an unpinned upgrade can change the artifact.
FYNE_CLI_VERSION := v1.26.1
FYNE := $(shell command -v fyne 2>/dev/null)
ANDROID_ICON := $(CURDIR)/assets/logo-512.png

# The Windows executable icon is a PE resource, not a file next to the exe: only
# a .syso in the package directory reaches the linker, and only the one named
# for the target GOOS/GOARCH. go-winres writes rsrc_windows_amd64.syso, so the
# Linux, macOS and Android builds never see it. The tool is pinned and run with
# `go run`, so it stays a build-time tool and never enters go.mod.
# The window binary's entry point. It is a package of its own — not the command
# tree — so a double-click can never depend on argument parsing (#382).
GUI_MAIN_DIR := gui

WINRES_VERSION := v0.3.3
WINRES_SRC := winres/winres.json
WINRES_SYSO := rsrc_windows_amd64.syso

# What every Unix archive carries, in the order it is listed inside the
# archive. The staging directory holds a copy of each file, so the binaries and
# the documents sit next to each other with no `../` paths in the recipe.
UNIX_ARCHIVE_FILES := $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE $(SCRIPT_NAME).sh

# What every Windows archive carries.
WINDOWS_ARCHIVE_FILES := $(APP_NAME).exe $(APP_NAME_SHORT).exe $(SCRIPT_NAME).bat README.md LICENSE


.PHONY: all windows linux android darwin zip_linux zip_windows zip_android clean release \
	gui-linux gui-windows gui-darwin gui-android release-gui check assets winres-icon require-mingw \
	help dev-up dev-down dev-shell dev-opencode dev-opencode-auto dev-log dev-build reset \
	preflight prepare-agent-mounts check-identity \
	shell opencode opencode-auto

all: release

# Create build folder if missing
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Build Windows binary
windows: $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(WINDOWS_BIN)
	cp $(WINDOWS_BIN) $(SHORT_WINDOWS_BIN)

# Build Linux binary
linux: $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(LINUX_BIN)
	cp $(LINUX_BIN) $(SHORT_LINUX_BIN)

# Build Android binary
android: $(BUILD_DIR)
	GOOS=android GOARCH=arm64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(ANDROID_BIN)
	cp $(ANDROID_BIN) $(SHORT_ANDROID_BIN)

# Compress Windows binary
zip_windows: windows
	zip -j $(WINDOWS_ZIP) $(WINDOWS_BIN) $(SHORT_WINDOWS_BIN) $(WINDOWS_SCRIPT) README.md LICENSE

# Compress Linux binary
zip_linux: linux
	tar -czvf $(LINUX_TAR) \
	          -C $(LINUX_BUILD) $(notdir $(LINUX_BIN)) $(notdir $(SHORT_LINUX_BIN)) \
	          -C ../../$(SCRIPTS_DIR) $(notdir $(LINUX_SCRIPT)) ../README.md ../LICENSE

# Compress Android binary
zip_android: android
	zip -j $(ANDROID_ZIP) $(ANDROID_BIN) $(SHORT_ANDROID_BIN) README.md LICENSE
	tar -czvf $(ANDROID_TAR) \
	          -C $(ANDROID_BUILD) $(notdir $(ANDROID_BIN)) $(notdir $(SHORT_ANDROID_BIN)) \
	          -C ../../ README.md LICENSE

# Clean build folder
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(WINRES_SYSO)

# Build & compress the CLI for every platform. `VERSION` may be overridden on
# the command line (`make release VERSION=v0.2.0`) — the release workflow does
# exactly that, because a shallow CI checkout has no tags for `git describe`.
release: zip_windows zip_linux zip_android darwin ## build every CLI archive (windows, linux, android, darwin)
	@echo "Release ready: $(VERSION)"

# ---------------------------------------------------------------------------
# macOS CLI
#
# CGO is off: nothing the CLI imports needs cgo, so both architectures
# cross-compile from Linux, macOS or Windows alike.
# ---------------------------------------------------------------------------
darwin: $(BUILD_DIR) ## build the macOS CLI archives (arm64, amd64)
	@mkdir -p $(DARWIN_ARM64_DIR) $(DARWIN_AMD64_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(DARWIN_ARM64_DIR)/$(APP_NAME)
	cp $(DARWIN_ARM64_DIR)/$(APP_NAME) $(DARWIN_ARM64_DIR)/$(APP_NAME_SHORT)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(DARWIN_AMD64_DIR)/$(APP_NAME)
	cp $(DARWIN_AMD64_DIR)/$(APP_NAME) $(DARWIN_AMD64_DIR)/$(APP_NAME_SHORT)
	@for arch in arm64 amd64; do \
	  dir="$(DARWIN_BUILD)/$$arch"; \
	  cp README.md LICENSE "$$dir/"; \
	  cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh "$$dir/"; \
	  tar -czf "$(DARWIN_BUILD)/$(APP_NAME)_$(VERSION)_darwin_$$arch.tar.gz" -C "$$dir" $(UNIX_ARCHIVE_FILES); \
	done

# ---------------------------------------------------------------------------
# Fyne GUI (`-tags gui`)
#
# Three targets, because the three platforms need three different build hosts
# or toolchains. None of them is part of `release`: a GUI build that silently
# fails, or that the local sandbox cannot run at all, would turn the release
# target into something that only works on one machine.
#
#   gui-linux    needs CGO + GTK3/X11/GL headers    (any Linux with them)
#   gui-windows  needs mingw-w64's x86_64-w64-mingw32-gcc
#   gui-darwin   must run on macOS; refused elsewhere
#   gui-android  needs the Android SDK + NDK and the fyne CLI
#
# The Android build number is derived from the version (0.11.3 -> 1103), not from
# the commit count: CI checks out shallow, where a count is always 1, and a
# versionCode that never changes makes an update indistinguishable from a
# reinstall. `gui-android` renders mobile/AndroidManifest.xml from its template
# first, so the
# APK is locked to portrait and still carries the release's version (#302); the
# generated file is removed afterwards and is not committed. `fyne package`
# without this target falls back to the CLI's own manifest — rotation allowed.
#
# `gui-android` produces a debug-signed APK with the Fyne toolchain: without
# `fyne package --release` the packager signs with its bundled debug key, which
# is what a sideload needs. `--release` would emit an .aab that only a store or
# bundletool can install, so it is deliberately not used here.
#
# `release-gui` is the Linux half only — the Windows cross-build and the macOS
# native build are separate because no single host can do all three. The
# release workflow runs each one on the runner that can.
# ---------------------------------------------------------------------------
gui-linux: $(BUILD_DIR) ## build the Linux Fyne GUI archive (needs GTK/X11/GL headers)
	@command -v pkg-config >/dev/null 2>&1 && pkg-config --exists gtk+-3.0 || { \
	  echo "ERROR: the GTK3/X11/GL development headers are missing — the Linux GUI needs them." >&2; \
	  echo "  Debian/Ubuntu: sudo apt-get install gcc libgl1-mesa-dev xorg-dev libgtk-3-dev libxkbcommon-dev" >&2; \
	  exit 1; }
	@mkdir -p $(GUI_LINUX_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION) -X $(INJECT_GUI_VERSION)=$(VERSION)" -o $(GUI_LINUX_DIR)/$(APP_NAME)_gui ./$(GUI_MAIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_LINUX_DIR)/$(APP_NAME)
	cp $(GUI_LINUX_DIR)/$(APP_NAME) $(GUI_LINUX_DIR)/$(APP_NAME_SHORT)
	cp README.md LICENSE $(GUI_LINUX_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh $(GUI_LINUX_DIR)/
	cp $(SCRIPTS_DIR)/cthrone.desktop $(GUI_LINUX_DIR)/ 2>/dev/null || true
	# No icon file goes into the archive: add_to_path.sh writes the launcher
	# icon from the data it carries, and the window icon is embedded in the
	# binary. A PNG next to the binaries would be read by nothing.
	tar -czf $(GUI_LINUX_TAR) -C $(GUI_LINUX_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE $(SCRIPT_NAME).sh cthrone.desktop

# The Windows GUI needs mingw-w64 for the CGO link, and the icon needs the Go
# resource tool below. Both are settled before a single file is generated, so a
# missing tool fails without leaving a stray .syso behind.
require-mingw:
	@command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 || { \
	  echo "ERROR: x86_64-w64-mingw32-gcc is missing — the Windows GUI needs mingw-w64." >&2; \
	  echo "  Debian/Ubuntu: sudo apt-get install gcc-mingw-w64-x86-64" >&2; \
	  exit 1; }

# Build the Windows icon resource the GUI exe is linked with, and fail loudly
# when it cannot be produced: an exe with no icon is exactly the bug this target
# exists to prevent, so a silent skip is worse than a red build.
winres-icon: ## generate the Windows GUI exe icon resource (first run needs module proxy access)
	@go run github.com/tc-hib/go-winres@$(WINRES_VERSION) make --in $(WINRES_SRC) --arch amd64 || { \
	  echo "ERROR: go-winres $(WINRES_VERSION) did not run — the Windows exe would carry no icon." >&2; \
	  echo "  It is fetched with 'go run', so the first use needs module proxy access." >&2; \
	  exit 1; }
	@test -f $(WINRES_SYSO) || { echo "ERROR: $(WINRES_SYSO) was not created." >&2; exit 1; }

# Windows reads an exe's icon from its PE resource, so the icon has to be linked
# in — a loose .ico beside the exe is never consulted, and shipping one only
# bloats the archive. The resource is created *after* the two CLI exes are built
# and removed right after the GUI exe: cthrone.exe and ct.exe are the
# command-line tool, and only the exe the user clicks carries the logo.
#
# The subsystem follows the same split, and it is not cosmetic. cthrone_gui.exe
# is linked -H=windowsgui, so a click opens the window with no console flashing
# behind it. The two CLI exes keep the console subsystem: a GUI-subsystem
# cthrone.exe shows nothing when double-clicked, makes cmd return before it
# finishes (so `errorlevel` is meaningless), and hides every log and panic line —
# a failed host looks like a silent no-op. release.yml checks the three values
# from the PE headers, so a regression fails the build instead of shipping.
gui-windows: $(BUILD_DIR) require-mingw ## cross-build the Windows Fyne GUI archive (needs mingw-w64)
	@mkdir -p $(GUI_WINDOWS_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_WINDOWS_DIR)/$(APP_NAME).exe
	cp $(GUI_WINDOWS_DIR)/$(APP_NAME).exe $(GUI_WINDOWS_DIR)/$(APP_NAME_SHORT).exe
	@$(MAKE) --no-print-directory winres-icon || { rm -f $(WINRES_SYSO); exit 1; }
	# The resource is a file of the package it is linked into, and the window is
	# now its own package: move it beside the entry point for the one build that
	# carries the icon, then take it away again.
	@mv $(WINRES_SYSO) $(GUI_MAIN_DIR)/$(WINRES_SYSO)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION) -X $(INJECT_GUI_VERSION)=$(VERSION) -H=windowsgui" -o $(GUI_WINDOWS_DIR)/$(APP_NAME)_gui.exe ./$(GUI_MAIN_DIR) || { rm -f $(GUI_MAIN_DIR)/$(WINRES_SYSO); exit 1; }
	@rm -f $(GUI_MAIN_DIR)/$(WINRES_SYSO)
	cp README.md LICENSE $(GUI_WINDOWS_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).bat $(GUI_WINDOWS_DIR)/
	cd $(GUI_WINDOWS_DIR) && zip -j $(abspath $(GUI_WINDOWS_ZIP)) $(APP_NAME)_gui.exe $(APP_NAME).exe $(APP_NAME_SHORT).exe README.md LICENSE $(SCRIPT_NAME).bat

gui-darwin: $(BUILD_DIR) ## build the macOS Fyne GUI archive (must run on macOS)
	@test "$$(go env GOOS)" = "darwin" || { \
	  echo "ERROR: gui-darwin must run on macOS — the Fyne darwin build needs the native toolchain." >&2; \
	  echo "  This target is built by the release workflow on a macos-latest runner." >&2; \
	  exit 1; }
	@mkdir -p $(GUI_DARWIN_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION) -X $(INJECT_GUI_VERSION)=$(VERSION)" -o $(GUI_DARWIN_DIR)/$(APP_NAME)_gui ./$(GUI_MAIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_DARWIN_DIR)/$(APP_NAME)
	cp $(GUI_DARWIN_DIR)/$(APP_NAME) $(GUI_DARWIN_DIR)/$(APP_NAME_SHORT)
	cp README.md LICENSE $(GUI_DARWIN_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh $(GUI_DARWIN_DIR)/
	# No cthrone.png: Finder reads an .icns inside a .app bundle, never a PNG
	# beside the binary, and the window icon comes from the embedded copy.
	tar -czf $(GUI_DARWIN_TAR) -C $(GUI_DARWIN_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE $(SCRIPT_NAME).sh

# Build the Android APK. Everything the packager needs is checked first: a
# missing SDK, NDK or fyne CLI fails the target with what to install, instead
# of a stack trace from deep inside the mobile build.
gui-android: $(BUILD_DIR) ## build the Android APK (needs Android SDK + NDK and the fyne CLI)
	@if [ -z "$(FYNE)" ]; then \
	  echo "ERROR: the fyne CLI is missing — it packages the APK." >&2; \
	  echo "  go install fyne.io/tools/cmd/fyne@$(FYNE_CLI_VERSION)" >&2; \
	  exit 1; \
	fi; \
	if [ -z "$${ANDROID_HOME:-}" ] && [ -n "$${ANDROID_SDK_ROOT:-}" ]; then \
	  ANDROID_HOME="$$ANDROID_SDK_ROOT"; export ANDROID_HOME; \
	fi; \
	if [ -z "$${ANDROID_HOME:-}" ] || [ ! -d "$$ANDROID_HOME" ]; then \
	  echo "ERROR: ANDROID_HOME (or ANDROID_SDK_ROOT) is not set to the Android SDK." >&2; \
	  exit 1; \
	fi; \
	if ! ls "$$ANDROID_HOME"/build-tools/*/aapt2 >/dev/null 2>&1; then \
	  echo "ERROR: aapt2 is missing from $$ANDROID_HOME/build-tools — install the Android build-tools." >&2; \
	  exit 1; \
	fi; \
	if [ -z "$${ANDROID_NDK_HOME:-}" ]; then \
	  if [ -d "$$ANDROID_HOME/ndk-bundle" ]; then \
	    ANDROID_NDK_HOME="$$ANDROID_HOME/ndk-bundle"; export ANDROID_NDK_HOME; \
	  else \
	    ndk="$$(ls -d "$$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1)"; \
	    if [ -n "$$ndk" ]; then ANDROID_NDK_HOME="$$ndk"; export ANDROID_NDK_HOME; fi; \
	  fi; \
	fi; \
	if [ -z "$${ANDROID_NDK_HOME:-}" ] || [ ! -d "$$ANDROID_NDK_HOME" ]; then \
	  echo "ERROR: no Android NDK found — set ANDROID_NDK_HOME, or install one under \$$ANDROID_HOME/ndk." >&2; \
	  exit 1; \
	fi; \
	version="$$(printf '%s' '$(VERSION)' | sed -E 's/^v//; s/[-+].*//')"; \
	case "$$version" in \
	  [0-9]*.[0-9]*.[0-9]*) : ;; \
	  *) echo "ERROR: '$(VERSION)' is not an x.y.z version — pass VERSION=v<major>.<minor>.<patch>." >&2; exit 1 ;; \
	esac; \
	build="$$(echo "$$version" | awk -F. '{printf "%d", $$1*10000 + $$2*100 + $$3}')"; \
	mkdir -p $(GUI_BUILD) && \
	sed -e "s/@VERSION@/$$version/g" -e "s/@BUILD@/$$build/g" \
	  mobile/AndroidManifest.xml.tmpl > mobile/AndroidManifest.xml && \
	printf '//go:build versionfile\n\npackage main\n\nvar buildVersion = "v%s"\n' "$$version" > mobile/version_build.go && \
	( cd mobile && $(FYNE) package \
	  -target android/arm64 \
	  -name $(APP_NAME) \
	  -app-id com.cthrone.app \
	  -icon $(ANDROID_ICON) \
	  -app-version "$$version" \
	  -app-build "$$build" \
	  -tags "gui versionfile" ) ; status=$$?; \
	rm -f mobile/AndroidManifest.xml mobile/version_build.go; \
	if [ $$status -ne 0 ]; then exit $$status; fi; \
	[ -f mobile/$(APP_NAME).apk ] && mv mobile/$(APP_NAME).apk $(abspath $(GUI_ANDROID_APK))

# The GUI archives a Linux machine with the headers can produce on its own.
release-gui: gui-linux gui-windows ## build the GUI archives a Linux host can produce (linux, windows)
	@echo "GUI release ready: $(VERSION)"
	@echo "  gui-darwin is built separately on macOS."

# ---------------------------------------------------------------------------
# Local verification gate
#
# gofmt, vet, test, build — in that order, because a formatting failure hides
# everything after it.
#
# The test stage runs twice: the plain tree, then the gui-tagged widget tree
# headless (`GUI_TEST`). The second run is what proves a transfer row and an
# inline image, not just that the GUI compiles.
#
# Two properties this target deliberately has:
#
#   - A missing toolchain FAILS the target. It does not skip. A gate that
#     cannot fail is worse than no gate: it reads green and proves nothing.
#   - After the run it names the packages that have no test files. `go test`
#     prints "[no test files]" for those and still exits 0, so a green run
#     would otherwise look like coverage that does not exist.
#
# Most packages have no test file, so most of them show up in that list. Treat
# the list as the uncovered surface, not as a pass.
#
#   - EVERY stage fails the target. The recipe runs under `set -e`, because a
#     stage chained with `;` hands its exit status to the next command and the
#     final `if` would report success over a failing vet, test or build. A gate
#     that cannot fail is worse than no gate.
#
#   - The Windows tree is cross-compiled too (CGO off). A file behind
#     `//go:build windows` is not compiled by a plain Linux `go build`, so an
#     unused import there stayed green here while the Windows release archives
#     could not be built at all (#176). This compiles that tree; the native
#     Fyne link stays the release workflow's job.
#
#   - The Android CLI tree is cross-compiled as well, for the reason above and
#     one more: GOOS=android satisfies the `linux` build tag, so a Linux-only
#     file (`//go:build linux`) is compiled there unless it excludes android
#     explicitly. That mistake cannot be seen from a Linux build; it is exactly
#     how the BlueZ transport would have entered the phone build (#202). The
#     scope stops short of `./...` because Fyne's mobile and darwin drivers
#     need CGO and platform glue, which is the release workflow's job.
# ---------------------------------------------------------------------------
check: ## gofmt, vet, test, build — the local verification gate
	@set -e; \
	if ! command -v go >/dev/null 2>&1; then \
		echo "ERROR: go is not installed — the check cannot run, so nothing is verified" >&2; \
		exit 1; \
	fi; \
	out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi; \
	go vet ./...; \
	go test ./...; \
	$(GUI_TEST); \
	go build ./...; \
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...; \
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build . ./cmd/... ./internal/...; \
	notested="$$(go list -f '{{if and (not .TestGoFiles) (not .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./... | grep -v '^$$' || true)"; \
	if [ -n "$$notested" ]; then \
		echo ""; \
		echo "NOTE: these packages have no test files — this run does not cover them:"; \
		echo "$$notested" | sed 's/^/  /'; \
	fi

test-gui: ## test the gui-tagged widget tree headless (no toolkit needed)
	$(GUI_TEST)

# `-race` is a separate target rather than part of `check` because it needs a C
# compiler, and `make check` must keep working on a host without one. CI runs
# it, so a race that appears in a change is caught there; a local run is one
# command.
#
# The gui-tagged suite is deliberately not in this run. Fyne's *test* driver
# executes fyne.Do inline, so a worker goroutine's UI write lands on the
# worker's goroutine while the test reads the widget — the detector reports a
# race in the harness, not in the program, and it cannot be silenced without a
# different driver. `go test -race ./...` here is the non-GUI build of the same
# code, which is where this project's concurrency lives anyway.
# `go test -fuzz` takes one target per run, so the target loops. The corpus that
# matters is the seed set in the test: a *failing* input is written to
# testdata/fuzz by the tool itself, and that file is what a later run replays
# before it starts exploring.
FUZZTIME ?= 20s
# The minimizer is bounded by executions rather than by time, and the bound is
# the point: `go test -fuzz` minimizes every newly interesting input with a
# `-fuzzminimizetime` budget, 60s by default — longer than the window above. On
# the bluetooth reassembler the minimizer keeps finding more to cut in a long
# packet stream, so the default spent the whole window minimizing: the run
# logged 63,856 executions at 3s, then 0/sec until the timer expired, and
# exited 0 anyway (#483). A gate that stops fuzzing after three seconds reports
# success without having exercised anything. `100x` keeps the reduction — a
# failing input is still minimized and still written to testdata/fuzz — and
# leaves the window for exploration. Raise it by hand for a long run where the
# smaller input is worth more than the coverage: `make fuzz FUZZMINIMIZE=60s`.
FUZZMINIMIZE ?= 100x
# The parsers of input this program did not write. `wire` is the envelope every
# frame goes through; `discovery`'s beacon is the one message anybody on the
# segment can send without a credential (#414); `e2e`'s handshake is the first
# frame a peer sends, before it has proved anything (#416); the bluetooth
# reassembler is the packet stream a radio peer writes, with no way to ask who
# wrote it (#418).
FUZZ_TARGETS := ./internal/wire:FuzzParse ./internal/wire:FuzzChunks \
	./internal/discovery:FuzzBeacon ./internal/e2e:FuzzHandshake \
	./internal/handlers/bluetooth:FuzzReassemble

fuzz: ## fuzz the parsers of untrusted input (FUZZTIME=20s by default)
	@set -e; \
	for target in $(FUZZ_TARGETS); do \
	  pkg="$${target%%:*}"; name="$${target##*:}"; \
	  echo "fuzzing $$name in $$pkg for $(FUZZTIME)"; \
	  CGO_ENABLED=0 go test -run '^$$' -fuzz "^$$name$$" \
	    -fuzztime=$(FUZZTIME) -fuzzminimizetime=$(FUZZMINIMIZE) "$$pkg"; \
	done

race: ## test with the race detector (needs a C compiler)
	@if [ "$$(CGO_ENABLED=1 go env CGO_ENABLED)" != "1" ] || ! echo 'int main(void){return 0;}' | $${CC:-cc} -x c - -o /dev/null 2>/dev/null; then \
	  echo "ERROR: -race needs cgo and a C compiler (CC=${CC:-cc}); neither is available here." >&2; \
	  echo "  This target cannot run, so it verifies nothing. Install a C compiler or run it in CI." >&2; \
	  exit 1; \
	fi
	CGO_ENABLED=1 go test -race -count=1 ./...

# ---------------------------------------------------------------------------
# Dev sandbox
#
# A pinned toolchain and a shell that behave the same on every machine. The
# sandbox is optional: the project still builds and tests with a plain host
# Go toolchain (see the `check` target above).
#
# The NATS broker lives in docker-compose.yml next to the sandbox, so
# `docker compose up -d nats` still gives you a broker and `docker compose up`
# still gives you the sandbox.
#
# Cross-platform note: this Makefile targets POSIX hosts — Linux, macOS and
# WSL2 — where `make` runs recipes under a POSIX shell. Native Windows users
# should use `python scripts/dev-up.py dev-up` (or `scripts/dev-up.ps1`), which
# provides the same targets without a POSIX shell. Everyone can run the project
# with a plain `docker compose up`, which needs no Make at all.
# ---------------------------------------------------------------------------

# --- Host identity -----------------------------------------------------------
# Run bind-mounted dev services as the host developer instead of root. Derived
# from the workspace OWNER, not the current process, so invoking make inside an
# already-running bind-mounted container still sends the host identity.
# GNU stat (-c) first, then BSD/macOS stat (-f), then plain `id`. The `id`
# fallback is what makes this work everywhere `make` does.
HOST_UID ?= $(shell stat -c '%u' . 2>/dev/null || stat -f '%u' . 2>/dev/null || id -u)
HOST_GID ?= $(shell stat -c '%g' . 2>/dev/null || stat -f '%g' . 2>/dev/null || id -g)
HOST_HOME ?= $(HOME)
export HOST_UID HOST_GID HOST_HOME

# --- In-container AI agents --------------------------------------------------
# opencode, Claude Code and Codex are installed in the dev image (pinned in
# Dockerfile.template), so they run on every host OS and nothing here depends on
# a host install. Each one asks for its own login on first run — an API key you
# paste, or an account you sign in with — and keeps it in a named volume
# (docker-compose.yml), so you sign in once per project, not once per start.
#
# Every agent has a plain target and an `-auto` target. The command lines are
# variables so a project can change one without touching the recipe:
#
#   opencode --auto            approves what is not explicitly denied; the
#                              secret-file deny rules in opencode.json still apply
#   claude --permission-mode auto
#                              approves what its safety review allows; the deny
#                              rules in .claude/settings.json still apply
#   codex --sandbox danger-full-access
#                              Codex's own sandbox (bubblewrap) cannot create a
#                              namespace inside an unprivileged container, so the
#                              CONTAINER is the sandbox and Codex still asks first
#   codex --dangerously-bypass-approvals-and-sandbox
#                              no prompts at all — the flag Codex documents for an
#                              externally sandboxed environment, which this is.
#                              Codex has no file deny list: in this mode nothing
#                              stops it reading `.env`. Use it where that is fine.
#
# Extra flags for one run: `make claude AGENT_ARGS="--model sonnet"`.
OPENCODE_CMD      ?= opencode
OPENCODE_AUTO_CMD ?= opencode --auto
CLAUDE_CMD        ?= claude
CLAUDE_AUTO_CMD   ?= claude --permission-mode auto
CODEX_CMD         ?= codex --sandbox danger-full-access
CODEX_AUTO_CMD    ?= codex --dangerously-bypass-approvals-and-sandbox
AGENT_ARGS        ?=

# The overlay carries only the host's gh config and ~/.gitconfig, and
# `prepare-agent-mounts` creates both sources first, so it is always merged.
COMPOSE_UP := docker compose -f docker-compose.yml -f docker-compose.agent.yml

# Python runs the agent-pin helper (`agents-upgrade`). Starting the sandbox
# never needs it. Resolved with fallbacks: a Windows Python often provides only
# `python` or the `py` launcher.
PYTHON ?= $(shell command -v python3 2>/dev/null || command -v python 2>/dev/null || command -v py 2>/dev/null)

# --- Is the dev image current? -----------------------------------------------
# `dev-up` does not rebuild, so an edited Dockerfile keeps running the old image
# until someone remembers `make dev-build`. The image therefore carries a label
# with a hash of the files it was built from, and the entry points refuse an
# image whose label does not match the files on disk.
#
# IMAGE_INPUTS lists the Dockerfile and EVERY file it COPYs. Add a file here
# when you add a COPY — `make check` compares the two. The hash is the first 16
# hex digits of SHA-256 over the files in this order; scripts/dev-up.py computes
# the same value, so both entry points agree on what "current" means.
IMAGE_INPUTS ?= Dockerfile scripts/dev-sandbox-rc.sh
IMAGE_INPUTS_HASH = $(shell cat $(IMAGE_INPUTS) 2>/dev/null | { sha256sum 2>/dev/null || shasum -a 256; } | cut -c1-16)
# Compose names a built image `<project>-<service>`; the project is `name:`.
DEV_IMAGE ?= $(shell sed -n 's/^name:[[:space:]]*//p' docker-compose.yml | head -n 1)-dev


help: ## List the available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
	@printf '\n  \033[36m%-30s\033[0m %s\n' "docker compose up" "run the sandbox with no host prerequisites (any OS)"
	@printf '  \033[36m%-30s\033[0m %s\n' "scripts/dev-up.py" "native Windows: python scripts/dev-up.py dev-up"

# --- Preflight ---------------------------------------------------------------
# Refuses to start as root, and pre-creates the read-only mount sources the
# agent overlay needs. Missing mount sources are fatal on Windows and create
# root-owned directories on Linux, so they are created here rather than assumed.
preflight: check-identity prepare-agent-mounts

check-identity:
	@test "$(HOST_UID)" != "0" || { echo "ERROR: refusing to run the dev sandbox as UID 0; repair workspace ownership or set HOST_UID to the host user." >&2; exit 1; }

prepare-agent-mounts:
	@mkdir -p "$(HOST_HOME)/.config/gh" && touch "$(HOST_HOME)/.gitconfig"

# Refuses a dev image that was built from different files than the ones on
# disk (see IMAGE_INPUTS). A missing image is built — by `dev-build`, so it
# carries the label. A mismatch is an error, not a rebuild: `dev-up` never
# rebuilds behind your back, it tells you to.
check-image:
	@have="$$(docker image inspect --format '{{ index .Config.Labels "dev.image-inputs" }}' '$(DEV_IMAGE)' 2>/dev/null || true)"; \
	if [ -z "$$have" ]; then \
	  echo "[sandbox] no dev image yet — building it (this runs once)."; \
	  $(MAKE) --no-print-directory dev-build; \
	elif [ "$$have" != "$(IMAGE_INPUTS_HASH)" ]; then \
	  echo "ERROR: the dev image is out of date: it was not built from the current $(IMAGE_INPUTS)." >&2; \
	  echo "       (an edited Dockerfile, a bumped agent or toolchain pin, or an image built outside make dev-build)" >&2; \
	  echo "       Rebuild it: make dev-build   — agent logins are kept, they live in volumes." >&2; \
	  exit 1; \
	fi

# The value the image label is compared with. Printed for the launcher parity
# check and for debugging a mismatch.
image-inputs-hash:
	@echo "$(IMAGE_INPUTS_HASH)"

# What the RUNNING sandbox has: each agent's version and whether it is signed
# in, then (with Python) each pin next to the vendor's latest release.
agents-version: ## Show agent versions and logins in the sandbox, and the latest releases
	@test -n "$$($(COMPOSE_UP) ps -q dev 2>/dev/null)" || { echo "ERROR: the dev sandbox is not running — start it with 'make dev-up'." >&2; exit 1; }
	@$(COMPOSE_UP) exec -T dev bash /workspace/scripts/agents-status.sh
	@if [ -n "$(PYTHON)" ]; then echo; $(PYTHON) scripts/agents-pin.py show; fi

# Upgrading an agent = rewrite its pin in the Dockerfile to the vendor's latest
# release (or AGENT_VERSION=X.Y.Z for one agent), rebuild, and re-create a
# running sandbox on the new image. The pin change is an ordinary diff to
# commit, so the whole project moves together. Logins live in volumes and
# survive all of it.
AGENTS_TO_UPGRADE ?=
AGENT_VERSION ?=

agents-upgrade: preflight ## Upgrade every agent to its latest release (keeps logins)
	@test -n "$(PYTHON)" || { echo "ERROR: agents-upgrade needs python3 on the host to look up and rewrite the pins." >&2; exit 1; }
	@$(PYTHON) scripts/agents-pin.py bump $(AGENTS_TO_UPGRADE) $(if $(AGENT_VERSION),--version $(AGENT_VERSION))
	@$(MAKE) --no-print-directory dev-build
	@if [ -n "$$($(COMPOSE_UP) ps -q dev 2>/dev/null)" ]; then \
	  echo "[agents] re-creating the running sandbox on the new image (logins are kept)..."; \
	  token="$$(scripts/resolve-gh-token.sh 2>/dev/null)"; \
	  GH_TOKEN="$$token" GITHUB_TOKEN="$$token" $(COMPOSE_UP) up -d --force-recreate dev; \
	  $(MAKE) --no-print-directory agents-version; \
	fi
	@echo "[agents] commit the Dockerfile pin change so the whole project upgrades with you."

opencode-upgrade: AGENTS_TO_UPGRADE = opencode
opencode-upgrade: agents-upgrade ## Upgrade opencode in the sandbox (AGENT_VERSION=X.Y.Z to pin one)
claude-upgrade: AGENTS_TO_UPGRADE = claude
claude-upgrade: agents-upgrade ## Upgrade Claude Code in the sandbox (AGENT_VERSION=X.Y.Z to pin one)
codex-upgrade: AGENTS_TO_UPGRADE = codex
codex-upgrade: agents-upgrade ## Upgrade Codex in the sandbox (AGENT_VERSION=X.Y.Z to pin one)

# One look at everything the sandbox depends on. A failed item names its fix.
# Logins are checked by each agent's own status command (exit code only) — no
# credential is ever printed.
doctor: ## Check Docker, the dev image, the sandbox, the GitHub token and each agent's login
	@status=0; \
	ok()   { printf '  ok    %s\n' "$$1"; }; \
	bad()  { printf '  FAIL  %s\n' "$$1"; status=1; }; \
	note() { printf '  --    %s\n' "$$1"; }; \
	if docker info >/dev/null 2>&1; then ok "Docker is running"; else bad "Docker is not reachable — start Docker Desktop or the docker service"; fi; \
	have="$$(docker image inspect --format '{{ index .Config.Labels "dev.image-inputs" }}' '$(DEV_IMAGE)' 2>/dev/null || true)"; \
	if [ -z "$$have" ]; then note "no dev image yet — make dev-up builds it"; \
	elif [ "$$have" = "$(IMAGE_INPUTS_HASH)" ]; then ok "dev image is current"; \
	else bad "dev image is out of date — make dev-build"; fi; \
	if [ -n "$$(scripts/resolve-gh-token.sh 2>/dev/null)" ]; then ok "GitHub token available (gh works in the sandbox)"; \
	else note "no GitHub token — gh auth login on the host"; fi; \
	if [ -n "$$($(COMPOSE_UP) ps -q dev 2>/dev/null)" ]; then \
	  ok "sandbox is running"; \
	  $(COMPOSE_UP) exec -T dev bash /workspace/scripts/agents-status.sh || status=1; \
	else note "sandbox is not running — make dev-up (agent logins are checked while it runs)"; fi; \
	exit $$status

# --- Sandbox lifecycle -------------------------------------------------------
# HOST_UID/HOST_GID need no explicit export here: docker-compose.yml defaults
# them, and the exported values above supply the host's real identity when make
# is running on the host. Only the GitHub token is resolved at up-time.
dev-up: preflight check-image ## Start the dev sandbox (foreground; Ctrl+C stops it)
	@GH_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) GITHUB_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) $(COMPOSE_UP) up dev

dev-down: ## Stop the dev sandbox (keeps volumes)
	@docker compose down

dev-log: ## Tail dev sandbox logs
	@docker compose logs -f dev

dev-build: check-identity ## Rebuild the dev image after Dockerfile/dependency changes
	@docker compose build --build-arg DEV_IMAGE_INPUTS=$(IMAGE_INPUTS_HASH) dev

reset: ## Stop everything and wipe volumes (clean slate)
	@docker compose down -v

shell: dev-shell
opencode: dev-opencode
opencode-auto: dev-opencode-auto
claude: dev-claude
claude-auto: dev-claude-auto
codex: dev-codex
codex-auto: dev-codex-auto

dev-shell: preflight check-image ## Open a shell inside the dev sandbox
	@$(COMPOSE_UP) exec dev bash || $(COMPOSE_UP) run --rm dev bash

.PHONY: dev-agent dev-opencode dev-opencode-auto dev-claude dev-claude-auto dev-codex dev-codex-auto \
        opencode opencode-auto claude claude-auto codex codex-auto agents-version agents-upgrade \
        opencode-upgrade claude-upgrade codex-upgrade doctor check-image image-inputs-hash \
        preflight check-identity prepare-agent-mounts

# One recipe for every agent; the targets below only choose AGENT_CMD (a
# target-specific variable, which make passes down to this prerequisite).
#
# The sandbox is started if it is not running, WITHOUT --force-recreate: a
# running sandbox is left alone, so an agent already working in it keeps its
# session. The agent's exit status is the recipe's — no fallback re-runs it in
# another shell when it exits non-zero.
dev-agent: preflight check-image
	@test -n "$(AGENT_CMD)" || { echo "ERROR: run an agent target (make opencode, make claude-auto, ...), not dev-agent." >&2; exit 1; }
	@if [ -z "$$($(COMPOSE_UP) ps -q dev 2>/dev/null)" ]; then \
	  echo "[agent] starting the dev sandbox..."; \
	  token="$$(scripts/resolve-gh-token.sh 2>/dev/null)"; \
	  GH_TOKEN="$$token" GITHUB_TOKEN="$$token" $(COMPOSE_UP) up -d dev; fi
	@bin="$(firstword $(AGENT_CMD))"; \
	 tty="-T"; if [ -t 0 ]; then tty="-it"; fi; \
	 $(COMPOSE_UP) exec $$tty dev zsh -lc "cd /workspace && if ! command -v $$bin >/dev/null; then echo \"ERROR: $$bin is not installed in this image — add it to SANDBOX_AGENTS in the Dockerfile, then make dev-build.\" >&2; exit 127; fi; exec $(AGENT_CMD) $(AGENT_ARGS)"

dev-opencode: AGENT_CMD = $(OPENCODE_CMD)
dev-opencode: dev-agent ## Run opencode inside the dev sandbox
dev-opencode-auto: AGENT_CMD = $(OPENCODE_AUTO_CMD)
dev-opencode-auto: dev-agent ## Run opencode in auto mode inside the dev sandbox
dev-claude: AGENT_CMD = $(CLAUDE_CMD)
dev-claude: dev-agent ## Run Claude Code inside the dev sandbox
dev-claude-auto: AGENT_CMD = $(CLAUDE_AUTO_CMD)
dev-claude-auto: dev-agent ## Run Claude Code in auto mode inside the dev sandbox
dev-codex: AGENT_CMD = $(CODEX_CMD)
dev-codex: dev-agent ## Run Codex inside the dev sandbox (the container is its sandbox)
dev-codex-auto: AGENT_CMD = $(CODEX_AUTO_CMD)
dev-codex-auto: dev-agent ## Run Codex with no approval prompts inside the dev sandbox

# Generate logo assets from SVG master
#
# ui/logo.png is the copy the GUI embeds (//go:embed cannot reach outside its
# own package), so it is refreshed here instead of being left to drift: the test
# in cmd/assets fails when the two differ.
assets: ## generate logo assets from SVG
	@go run ./cmd/assets
	@cp $(ASSETS_DIR)/logo-512.png ui/logo.png
	@echo "Assets generated in assets/ (and ui/logo.png refreshed)"
