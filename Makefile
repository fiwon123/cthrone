APP_NAME := cthrone
APP_NAME_SHORT := ct
BUILD_DIR := build
SCRIPTS_DIR := scripts
ASSETS_DIR := assets
SCRIPT_NAME := add_to_path
INJECT_VERSION:= github.com/fiwon123/cthrone/cmd.Version

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

# The fyne CLI that packages the APK. Pinned: the packager shapes the Android
# manifest and the signature, so an unpinned upgrade can change the artifact.
FYNE_CLI_VERSION := v1.26.1
FYNE := $(shell command -v fyne 2>/dev/null)
ANDROID_ICON := $(CURDIR)/assets/logo-512.png

# What every Unix archive carries, in the order it is listed inside the
# archive. The staging directory holds a copy of each file, so the binaries and
# the documents sit next to each other with no `../` paths in the recipe.
UNIX_ARCHIVE_FILES := $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).sh

# What every Windows archive carries.
WINDOWS_ARCHIVE_FILES := $(APP_NAME).exe $(APP_NAME_SHORT).exe $(SCRIPT_NAME).bat README.md LICENSE LICENSE-APACHE NOTICE


.PHONY: all windows linux android darwin zip_linux zip_windows zip_android clean release \
	gui-linux gui-windows gui-darwin gui-android release-gui check assets \
	help dev-up dev-down dev-shell dev-opencode dev-opencode-auto dev-log dev-build reset \
	preflight require-agent prepare-agent-mounts check-identity opencode-version opencode-upgrade \
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
	zip -j $(WINDOWS_ZIP) $(WINDOWS_BIN) $(SHORT_WINDOWS_BIN) $(WINDOWS_SCRIPT) README.md LICENSE LICENSE-APACHE NOTICE

# Compress Linux binary
zip_linux: linux
	tar -czvf $(LINUX_TAR) \
	          -C $(LINUX_BUILD) $(notdir $(LINUX_BIN)) $(notdir $(SHORT_LINUX_BIN)) \
	          -C ../../$(SCRIPTS_DIR) $(notdir $(LINUX_SCRIPT)) ../README.md ../LICENSE ../LICENSE-APACHE ../NOTICE

# Compress Android binary
zip_android: android
	zip -j $(ANDROID_ZIP) $(ANDROID_BIN) $(SHORT_ANDROID_BIN) README.md LICENSE LICENSE-APACHE NOTICE
	tar -czvf $(ANDROID_TAR) \
	          -C $(ANDROID_BUILD) $(notdir $(ANDROID_BIN)) $(notdir $(SHORT_ANDROID_BIN)) \
	          -C ../../ README.md LICENSE LICENSE-APACHE NOTICE

# Clean build folder
clean:
	rm -rf $(BUILD_DIR)

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
	  cp README.md LICENSE LICENSE-APACHE NOTICE "$$dir/"; \
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
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_LINUX_DIR)/$(APP_NAME)_gui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_LINUX_DIR)/$(APP_NAME)
	cp $(GUI_LINUX_DIR)/$(APP_NAME) $(GUI_LINUX_DIR)/$(APP_NAME_SHORT)
	cp README.md LICENSE LICENSE-APACHE NOTICE $(GUI_LINUX_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh $(GUI_LINUX_DIR)/
	cp $(SCRIPTS_DIR)/cthrone.desktop $(GUI_LINUX_DIR)/ 2>/dev/null || true
	cp $(ASSETS_DIR)/logo-512.png $(GUI_LINUX_DIR)/cthrone.png 2>/dev/null || cp $(ASSETS_DIR)/logo-128.png $(GUI_LINUX_DIR)/cthrone.png 2>/dev/null || true
	tar -czf $(GUI_LINUX_TAR) -C $(GUI_LINUX_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).sh cthrone.desktop cthrone.png 2>/dev/null || tar -czf $(GUI_LINUX_TAR) -C $(GUI_LINUX_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).sh cthrone.desktop cthrone.png

gui-windows: $(BUILD_DIR) ## cross-build the Windows Fyne GUI archive (needs mingw-w64)
	@command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 || { \
	  echo "ERROR: x86_64-w64-mingw32-gcc is missing — the Windows GUI needs mingw-w64." >&2; \
	  echo "  Debian/Ubuntu: sudo apt-get install gcc-mingw-w64-x86-64" >&2; \
	  exit 1; }
	@mkdir -p $(GUI_WINDOWS_DIR)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION) -H=windowsgui" -o $(GUI_WINDOWS_DIR)/$(APP_NAME)_gui.exe
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION) -H=windowsgui" -o $(GUI_WINDOWS_DIR)/$(APP_NAME).exe
	cp $(GUI_WINDOWS_DIR)/$(APP_NAME).exe $(GUI_WINDOWS_DIR)/$(APP_NAME_SHORT).exe
	cp README.md LICENSE LICENSE-APACHE NOTICE $(GUI_WINDOWS_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).bat $(GUI_WINDOWS_DIR)/
	cp $(ASSETS_DIR)/logo.ico $(GUI_WINDOWS_DIR)/cthrone.ico 2>/dev/null || true
	cp $(ASSETS_DIR)/logo.ico $(GUI_WINDOWS_DIR)/$(APP_NAME)_gui.ico 2>/dev/null || true
	cd $(GUI_WINDOWS_DIR) && zip -j $(abspath $(GUI_WINDOWS_ZIP)) $(APP_NAME)_gui.exe $(APP_NAME).exe $(APP_NAME_SHORT).exe README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).bat cthrone.ico $(APP_NAME)_gui.ico 2>/dev/null || cd $(GUI_WINDOWS_DIR) && zip -j $(abspath $(GUI_WINDOWS_ZIP)) $(APP_NAME)_gui.exe $(APP_NAME).exe $(APP_NAME_SHORT).exe README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).bat cthrone.ico

gui-darwin: $(BUILD_DIR) ## build the macOS Fyne GUI archive (must run on macOS)
	@test "$$(go env GOOS)" = "darwin" || { \
	  echo "ERROR: gui-darwin must run on macOS — the Fyne darwin build needs the native toolchain." >&2; \
	  echo "  This target is built by the release workflow on a macos-latest runner." >&2; \
	  exit 1; }
	@mkdir -p $(GUI_DARWIN_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -tags gui -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_DARWIN_DIR)/$(APP_NAME)_gui
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-X $(INJECT_VERSION)=$(VERSION)" -o $(GUI_DARWIN_DIR)/$(APP_NAME)
	cp $(GUI_DARWIN_DIR)/$(APP_NAME) $(GUI_DARWIN_DIR)/$(APP_NAME_SHORT)
	cp README.md LICENSE LICENSE-APACHE NOTICE $(GUI_DARWIN_DIR)/
	cp $(SCRIPTS_DIR)/$(SCRIPT_NAME).sh $(GUI_DARWIN_DIR)/
	cp $(ASSETS_DIR)/logo-512.png $(GUI_DARWIN_DIR)/cthrone.png 2>/dev/null || true
	tar -czf $(GUI_DARWIN_TAR) -C $(GUI_DARWIN_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).sh cthrone.png 2>/dev/null || tar -czf $(GUI_DARWIN_TAR) -C $(GUI_DARWIN_DIR) $(APP_NAME)_gui $(APP_NAME) $(APP_NAME_SHORT) README.md LICENSE LICENSE-APACHE NOTICE $(SCRIPT_NAME).sh cthrone.png

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
	build="$$(git rev-list --count HEAD 2>/dev/null || echo 1)"; \
	mkdir -p $(GUI_BUILD) && \
	cd mobile && $(FYNE) package \
	  -target android/arm64 \
	  -name $(APP_NAME) \
	  -app-id com.cthrone.app \
	  -icon $(ANDROID_ICON) \
	  -app-version "$$version" \
	  -app-build "$$build" \
	  -tags gui && \
	mv $(APP_NAME).apk $(abspath $(GUI_ANDROID_APK))

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
	go build ./...; \
	notested="$$(go list -f '{{if and (not .TestGoFiles) (not .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./... | grep -v '^$$' || true)"; \
	if [ -n "$$notested" ]; then \
		echo ""; \
		echo "NOTE: these packages have no test files — this run does not cover them:"; \
		echo "$$notested" | sed 's/^/  /'; \
	fi

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

# Extra flags for the opencode invocation. `make opencode-auto` /
# `make dev-opencode-auto` set this to --auto, which auto-approves permissions
# that are not explicitly denied; the secret-file deny rules still apply.
OPENCODE_ARGS ?=

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

# --- Optional in-container agent ---------------------------------------------
# Starting the sandbox never requires opencode. The agent layer is optional so
# `make dev-up` works on macOS and Windows too, where the host binary cannot
# execute inside the Linux container.
#
# NOTE: not named OPENCODE — the opencode runtime exports an OPENCODE env var
# (=1), which would override a `?=` default through make's env import.
OPENCODE_BIN ?= $(HOME)/.opencode/bin/opencode

# The dev container is Linux, so only an ELF binary can execute inside it. A
# macOS Mach-O or Windows PE install is detected here and skipped, instead of
# being mounted and failing later with "exec format error".
OPENCODE_MAGIC := $(shell head -c 4 "$(OPENCODE_BIN)" 2>/dev/null | od -An -tx1 2>/dev/null | tr -d ' \n')
AGENT_AVAILABLE := $(if $(filter 7f454c46,$(OPENCODE_MAGIC)),yes,no)

ifeq ($(AGENT_AVAILABLE),yes)
COMPOSE_UP := docker compose -f docker-compose.yml -f docker-compose.agent.yml
else
COMPOSE_UP := docker compose
endif

help: ## List the available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
	@printf '\n  \033[36m%-30s\033[0m %s\n' "docker compose up" "run the sandbox with no host prerequisites (any OS)"
	@printf '  \033[36m%-30s\033[0m %s\n' "scripts/dev-up.py" "native Windows: python scripts/dev-up.py dev-up"

# --- Preflight ---------------------------------------------------------------
# Refuses to start as root, and pre-creates the read-only mount sources the
# agent overlay needs. Missing mount sources are fatal on Windows and create
# root-owned directories on Linux, so they are created here rather than assumed.
# This target NEVER fails on a missing or non-Linux agent binary: that is what
# `require-agent` is for, and it is depended on only by the agent targets.
preflight: check-identity prepare-agent-mounts
	@if [ "$(AGENT_AVAILABLE)" = "yes" ]; then \
	  echo "[sandbox] in-container agent available — merging docker-compose.agent.yml."; \
	else \
	  echo "[sandbox] in-container agent unavailable (no Linux binary at $(OPENCODE_BIN))." >&2; \
	  echo "[sandbox]   Starting the sandbox without it." >&2; \
	fi

check-identity:
	@test "$(HOST_UID)" != "0" || { echo "ERROR: refusing to run the dev sandbox as UID 0; repair workspace ownership or set HOST_UID to the host user." >&2; exit 1; }

prepare-agent-mounts:
	@if [ "$(AGENT_AVAILABLE)" = "yes" ]; then \
	  mkdir -p "$(HOST_HOME)/.config/opencode" "$(HOST_HOME)/.config/gh" && touch "$(HOST_HOME)/.gitconfig"; fi

# Only the agent-specific targets need the in-container binary.
require-agent:
	@test "$(AGENT_AVAILABLE)" = "yes" || { \
	  echo "ERROR: the in-container AI agent needs a Linux opencode binary at $(OPENCODE_BIN)." >&2; \
	  echo "  A macOS/Windows binary cannot execute inside the Linux dev container." >&2; \
	  echo "  Recommended: use WSL2, where the host binary is Linux." >&2; \
	  echo "  Alternative: run 'opencode' on the host against the running stack." >&2; \
	  exit 1; }

# The host owns the opencode binary; the dev sandbox mounts it read-only
# (`~/.opencode/bin/opencode -> /usr/local/bin/opencode:ro`), so it can only be
# upgraded here, on the host. A *file* bind mount pins the inode, so a running
# sandbox keeps the old binary until it restarts — `opencode-upgrade` restarts
# it for you. Override the install URL if your installer differs.
OPENCODE_INSTALL_URL ?= https://opencode.ai/install

opencode-version: ## Print the host opencode version
	@command -v opencode >/dev/null 2>&1 || { echo "ERROR: opencode not on PATH — install it from $(OPENCODE_INSTALL_URL)" >&2; exit 1; }
	@opencode --version

opencode-upgrade: ## Upgrade host opencode and refresh the dev sandbox
	@command -v opencode >/dev/null 2>&1 || { echo "ERROR: opencode not on PATH — install it from $(OPENCODE_INSTALL_URL)" >&2; exit 1; }
	@echo "opencode before: $$(opencode --version 2>/dev/null || echo unknown)"
	@opencode upgrade </dev/null || { echo "opencode upgrade unavailable — falling back to the install script"; curl -fsSL $(OPENCODE_INSTALL_URL) | bash; }
	@echo "opencode after:  $$(opencode --version 2>/dev/null || echo unknown)"
	@if [ -n "$$(docker compose ps -q dev 2>/dev/null)" ]; then \
	  echo "restarting the dev sandbox so the bind-mounted binary refreshes..."; \
	  docker compose restart dev; \
	else \
	  echo "dev sandbox not running — nothing to refresh"; \
	fi

# --- Sandbox lifecycle -------------------------------------------------------
# HOST_UID/HOST_GID need no explicit export here: docker-compose.yml defaults
# them, and the exported values above supply the host's real identity when make
# is running on the host. Only the GitHub token is resolved at up-time.
dev-up: preflight ## Start the dev sandbox (foreground; Ctrl+C stops it)
	@GH_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) GITHUB_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) $(COMPOSE_UP) up --build dev

dev-down: ## Stop the dev sandbox (keeps volumes)
	@docker compose down

dev-log: ## Tail dev sandbox logs
	@docker compose logs -f dev

dev-build: check-identity ## Rebuild the dev image after Dockerfile/dependency changes
	@docker compose build dev

reset: ## Stop everything and wipe volumes (clean slate)
	@docker compose down -v

shell: dev-shell
opencode: dev-opencode
opencode-auto: dev-opencode-auto

dev-shell: preflight ## Open a shell inside the dev sandbox
	@$(COMPOSE_UP) exec dev bash || $(COMPOSE_UP) run --rm dev bash

dev-opencode: preflight require-agent ## Run the AI coding agent inside the dev sandbox
	@if [ -z "$$(docker compose ps -q dev 2>/dev/null)" ]; then echo "[opencode] starting dev stack..."; GH_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) GITHUB_TOKEN=$$(scripts/resolve-gh-token.sh 2>/dev/null) $(COMPOSE_UP) up -d dev; fi
	@if [ -t 0 ]; then $(COMPOSE_UP) exec -it dev zsh -lc "cd /workspace && opencode $(OPENCODE_ARGS)" || $(COMPOSE_UP) exec -it dev bash -lc "opencode $(OPENCODE_ARGS)"; else $(COMPOSE_UP) exec -T dev zsh -lc "cd /workspace && opencode $(OPENCODE_ARGS)" || $(COMPOSE_UP) exec -T dev bash -lc "opencode $(OPENCODE_ARGS)"; fi

# Same recipe, with --auto: auto-approve permissions that are not explicitly
# denied (every secret-file deny rule still applies).
dev-opencode-auto: OPENCODE_ARGS = --auto
dev-opencode-auto: dev-opencode

# Generate logo assets from SVG master
assets: ## generate logo assets from SVG
	@go run ./cmd/assets
	@echo "Assets generated in assets/"
