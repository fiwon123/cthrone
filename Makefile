APP_NAME := cthrone
APP_NAME_SHORT := ct
BUILD_DIR := build
SCRIPTS_DIR := scripts
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


.PHONY: all windows linux android zip_linux zip_windows zip_android clean release check

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

# Build & compress everything
release: zip_windows zip_linux zip_android
	@echo "Release ready: $(VERSION)"

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
# This repository has no test file at all yet, so every package shows up in
# that list. That is the state of the suite, not a pass.
# ---------------------------------------------------------------------------
check: ## gofmt, vet, test, build — the local verification gate
	@if ! command -v go >/dev/null 2>&1; then \
		echo "ERROR: go is not installed — the check cannot run, so nothing is verified" >&2; \
		exit 1; \
	fi; \
	out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi; \
	go vet ./...; \
	go test ./...; \
	go build ./...; \
	notested="$$(go list -f '{{if and (not .TestGoFiles) (not .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./... | grep -v '^$$')"; \
	if [ -n "$$notested" ]; then \
		echo ""; \
		echo "NOTE: these packages have no test files — this run does not cover them:"; \
		echo "$$notested" | sed 's/^/  /'; \
	fi
