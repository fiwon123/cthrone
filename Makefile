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


.PHONY: all windows linux android zip_linux zip_windows clean release

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
