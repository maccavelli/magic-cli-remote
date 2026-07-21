MODULE  := github.com/maccavelli/magic-cli-remote
# Version stamping (see scripts/next-build-version.sh):
#   release tag v0.2.1 → builds 0.2.1.1, 0.2.1.2, … claimed via git tags build/0.2.1.N
#   CI pushes those tags so local + CI share one monotonic ledger.
# Override: make build VERSION=1.2.3
BASE_VERSION ?= $(shell git tag -l 'v*.*.*' 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -1 | sed 's/^v//' || echo 0.0.0)
BUILD_COUNTER_FILE := .build-counter
NEXT_VERSION_SH := scripts/next-build-version.sh
# -dirty marks binaries built from a modified tree (tracked changes, same rule
# as `git describe --dirty`) — otherwise a dirty build is indistinguishable
# from a clean build of HEAD when debugging from the version string.
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)$(shell git diff-index --quiet HEAD -- 2>/dev/null || echo -dirty)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# True when the user passed VERSION=... on the command line.
VERSION_FROM_CLI := $(filter command line,$(origin VERSION))

# Host OS/arch → GOOS/GOARCH and user bin dir (override with GOOS=, GOARCH=, USER_BIN_DIR=).
UNAME_S := $(shell uname -s 2>/dev/null || echo unknown)
UNAME_M := $(shell uname -m 2>/dev/null || echo unknown)

ifeq ($(UNAME_S),Linux)
  GOOS   ?= linux
  # XDG user executables: ~/.local/bin
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifeq ($(UNAME_S),Darwin)
  GOOS   ?= darwin
  # Match Linux/XDG and setup-service default (~/.local/bin/mcremote).
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring MINGW,$(UNAME_S)))
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring MSYS,$(UNAME_S)))
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring CYGWIN,$(UNAME_S)))
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else
  GOOS   ?= $(shell go env GOOS 2>/dev/null || echo linux)
  USER_BIN_DIR ?= $(HOME)/.local/bin
endif

ifeq ($(UNAME_M),x86_64)
  GOARCH ?= amd64
else ifeq ($(UNAME_M),amd64)
  GOARCH ?= amd64
else ifeq ($(UNAME_M),aarch64)
  GOARCH ?= arm64
else ifeq ($(UNAME_M),arm64)
  GOARCH ?= arm64
else ifeq ($(UNAME_M),armv7l)
  GOARCH ?= arm
else ifeq ($(UNAME_M),i386)
  GOARCH ?= 386
else ifeq ($(UNAME_M),i686)
  GOARCH ?= 386
else
  GOARCH ?= $(shell go env GOARCH 2>/dev/null || echo amd64)
endif

# Windows binaries need .exe; install name stays mcremote[.exe].
ifeq ($(GOOS),windows)
  BIN_EXT := .exe
else
  BIN_EXT :=
endif
BIN := bin/mcremote$(BIN_EXT)
INSTALL_NAME := mcremote$(BIN_EXT)
INSTALL_PATH := $(USER_BIN_DIR)/$(INSTALL_NAME)
# systemd user unit name (best-effort stop/restart around install)
SERVICE_NAME ?= mcremote

.PHONY: build install test race test-all preflight apk install-hooks run fmt vet tidy clean

build:
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH))…"; \
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags "-X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN) ./cmd/mcremote

# Build for this host OS/arch and install into the user bin dir
# (Linux/macOS: ~/.local/bin). Override: make install USER_BIN_DIR=/some/path
#
# Avoids ETXTBSY / "text file busy" when mcremote is already running:
#   1. best-effort systemctl --user stop
#   2. write to a temp path, move existing aside, atomic rename into place
#   3. best-effort systemctl --user try-restart
install: build
	@mkdir -p "$(USER_BIN_DIR)"
	@set -e; \
	DEST="$(INSTALL_PATH)"; \
	NEW="$$DEST.new.$$$$"; \
	PREV="$$DEST.prev.$$$$"; \
	STOPPED=0; \
	if command -v systemctl >/dev/null 2>&1; then \
		if systemctl --user is-active --quiet "$(SERVICE_NAME).service" 2>/dev/null; then \
			echo "Stopping $(SERVICE_NAME).service for install…"; \
			systemctl --user stop "$(SERVICE_NAME).service" || true; \
			STOPPED=1; \
		fi; \
	fi; \
	install -m 755 "$(BIN)" "$$NEW"; \
	if [ -e "$$DEST" ] || [ -L "$$DEST" ]; then \
		mv -f "$$DEST" "$$PREV"; \
	fi; \
	mv -f "$$NEW" "$$DEST"; \
	rm -f "$$PREV"; \
	if [ "$$STOPPED" = "1" ]; then \
		echo "Restarting $(SERVICE_NAME).service…"; \
		systemctl --user start "$(SERVICE_NAME).service" || \
			systemctl --user try-restart "$(SERVICE_NAME).service" || true; \
	fi; \
	echo "Installed $$DEST ($(GOOS)/$(GOARCH))"; \
	"$(BIN)" version 2>/dev/null || true

test:
	go test ./...

race:
	go test -race ./...

test-all:
	@echo "Running Go tests..."
	go test -race ./...
	@echo "Running Flutter tests..."
	@cd apps/mobile && flutter test

# Reproduce the GitHub CI checks locally before pushing: the same Go vet+tests
# and Flutter analyze+tests the `go` and `flutter` jobs run. The phone app and
# release binaries are built on GitHub only on a version tag, so run this (and
# `make apk` if you touched the app) before opening/merging a PR.
preflight:
	@echo "==> go vet";        go vet ./...
	@echo "==> go test";       go test ./...
	@echo "==> flutter analyze"; cd apps/mobile && flutter analyze
	@echo "==> flutter test";  cd apps/mobile && flutter test
	@echo "✅ preflight passed"

# Build the release Android APK locally (arm64) for on-device testing. Debug-
# signed unless apps/mobile/android/key.properties is present; the signed,
# published release APK is produced by CI on a version tag.
# Output: apps/mobile/build/app/outputs/flutter-apk/app-release.apk
apk:
	cd apps/mobile && flutter build apk --release --target-platform android-arm64

install-hooks:
	@echo "Installing git pre-commit hook..."
	@cp scripts/pre-commit.sh .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@chmod +x scripts/pre-commit.sh
	@echo "Git pre-commit hook installed."

run:
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Running mcremote $$VER…"; \
	go run -ldflags "-X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		./cmd/mcremote serve

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

