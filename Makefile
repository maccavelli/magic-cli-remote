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

# Release-grade build settings — shared by build, build-remote, build-relay so
# every mcremote/mcrelay artifact is identical in flags.
#   CGO_ENABLED=0        → pure-Go, fully static binary: no libc, no dynamic
#                          loader; runs on any kernel of the target GOOS/GOARCH.
#                          Also what makes the cross-compiles just work.
#   -tags netgo,osusergo → force the pure-Go DNS resolver (netgo) and /etc/passwd
#                          user lookup (osusergo). Under CGO=0 this is already the
#                          behavior; the tags PIN it so the binary stays static
#                          even on a `make build CGO_ENABLED=1` override. net,
#                          os/user, runtime/cgo are the only cgo-capable deps.
#   -trimpath            → strip absolute build paths → reproducible builds.
#   -ldflags -s -w       → drop the symbol table and DWARF debug info (smaller
#                          binary; panics still carry full stack traces).
# Go's compiler optimizations and inlining are ON by default and nothing here
# passes -gcflags '-N -l', so they stay on; -s -w removes only debugger metadata,
# never optimizations. Override for a cgo build: make build CGO_ENABLED=1.
CGO_ENABLED   ?= 0
GO_TAGS       := netgo,osusergo
GO_BUILDFLAGS := -trimpath -tags $(GO_TAGS)
GO_LDFLAGS    := -s -w

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
BIN_RELAY := bin/mcrelay$(BIN_EXT)
INSTALL_NAME := mcremote$(BIN_EXT)
INSTALL_PATH := $(USER_BIN_DIR)/$(INSTALL_NAME)
# systemd user unit names (best-effort stop/restart around install)
SERVICE_NAME ?= mcremote
RELAY_SERVICE_NAME ?= mcrelay

# Android profile targets (see docs/mobile-profiling.md).
# DEVICE=  Flutter device id from `flutter devices` / `make profile-devices`.
DEVICE ?=
MOBILE_DIR := apps/mobile

.PHONY: build build-relay build-remote install install-relay test live-opencode race test-all preflight apk \
	verify-units profile profile-apk profile-devices install-hooks verify-hooks run fmt lint vulncheck \
	pre-add-check vet tidy clean

build:
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH), static, cgo=$(CGO_ENABLED))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN) ./cmd/mcremote; \
	echo "Building mcrelay $$VER…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN_RELAY) ./cmd/mcrelay

build-relay:
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Building mcrelay $$VER ($(GOOS)/$(GOARCH))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN_RELAY) ./cmd/mcrelay

# Build ONLY mcremote for one GOOS/GOARCH (mirror of build-relay). Lets the
# release job cross-compile each daemon over its own platform matrix, since
# mcremote and mcrelay ship different target sets. Pass VERSION= to reuse an
# already-allocated serial without touching the build/* ledger.
build-remote:
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN) ./cmd/mcremote

# Build for this host OS/arch and install BOTH mcremote and mcrelay into the
# user bin dir (Linux/macOS: ~/.local/bin). Override: make install USER_BIN_DIR=/some/path
#
# Both swaps go through scripts/install-binary.sh, which avoids ETXTBSY on a
# running binary (stage to a temp path, atomic rename) and — the part that
# matters — cannot strand a stopped daemon: it stages before it stops anything,
# and restores the unit from a trap on every exit path including Ctrl-C. It
# also starts an enabled-but-stopped unit, so a service left dead by an earlier
# failed install heals on the next `make install`.
install: build
	@mkdir -p "$(USER_BIN_DIR)"
	@./scripts/install-binary.sh "$(BIN)" "$(INSTALL_PATH)" "$(SERVICE_NAME)"
	@"$(BIN)" version 2>/dev/null || true
	@./scripts/install-binary.sh "$(BIN_RELAY)" "$(USER_BIN_DIR)/mcrelay$(BIN_EXT)" \
		"$(RELAY_SERVICE_NAME)"
	@"$(BIN_RELAY)" version 2>/dev/null || true

# Install mcrelay next to mcremote (does not stop/start a unit by default).
install-relay: build-relay
	@mkdir -p "$(USER_BIN_DIR)"
	@./scripts/install-binary.sh "$(BIN_RELAY)" "$(USER_BIN_DIR)/mcrelay$(BIN_EXT)"
	@"$(BIN_RELAY)" version 2>/dev/null || true

test:
	go test ./...

# Live OpenCode HTTP suite (MADR 0020 Sprint 4 / A6). Requires `opencode` on
# PATH and network access to whatever model the engine uses (often free Zen).
# Not part of default `make test` / CI — best-effort; subagent/todo cases skip
# when the model does not cooperate. Timeout covers cold start + multi-turn.
live-opencode:
	go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 600s -v

race:
	go test -race ./...

test-all:
	@echo "Running Go tests..."
	go test -race ./...
	@echo "Running Flutter tests..."
	@cd apps/mobile && flutter test

# Reproduce the GitHub CI checks locally before pushing. This mirrors the `go`
# and `flutter` jobs gate-for-gate — including the format, tidy, race, unit-file
# and allocator checks that CI enforces — so a green preflight means a green CI.
# The APK is built on GitHub only on a version tag; run `make apk` too if you
# touched the app.
#
# The release-binary build runs with the version ledger disabled: preflight
# proves the ldflags/cross-compile path still works without claiming a
# build/<BASE>.<N> serial or pushing a tag.
preflight:
	@set -e; \
	echo "==> gofmt"; \
	unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt drift (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi
	@set -e; \
	echo "==> go mod tidy"; \
	cp go.mod .go.mod.pre && cp go.sum .go.sum.pre; \
	go mod tidy; \
	rc=0; \
	if ! diff -q go.mod .go.mod.pre >/dev/null || ! diff -q go.sum .go.sum.pre >/dev/null; then \
		echo "go.mod/go.sum not tidy — commit the result of 'make tidy'"; rc=1; \
	fi; \
	mv -f .go.mod.pre go.mod; mv -f .go.sum.pre go.sum; \
	exit $$rc
	@echo "==> go vet";        go vet ./...
	@echo "==> go test -race"; go test -race ./...
	@echo "==> version allocator tests"; ./scripts/next-build-version_test.sh
	@echo "==> systemd units"; \
	if command -v systemd-analyze >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory verify-units; \
	else \
		echo "(skipped: systemd-analyze not installed)"; \
	fi
	@echo "==> release build (mcremote + mcrelay, no ledger write)"; \
	MCREMOTE_VERSION_PUSH=0 MCREMOTE_VERSION_TAG=0 $(MAKE) --no-print-directory build >/dev/null
	@./bin/mcremote version
	@./bin/mcrelay version
	@echo "==> dart format";  cd apps/mobile && dart format --output=none --set-exit-if-changed .
	@echo "==> flutter analyze"; cd apps/mobile && flutter analyze
	@echo "==> flutter test";  cd apps/mobile && flutter test
	@echo "✅ preflight passed"

# Validate the shipped systemd units. systemd-analyze also insists ExecStart
# exists, so point the units at binaries that are actually installed here —
# we are checking directives, not this machine's layout.
verify-units:
	@set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	rc=0; \
	for unit in deploy/systemd/*.service; do \
		out="$$tmp/$$(basename $$unit)"; \
		sed -e "s#/usr/local/bin/#$$tmp/#g" -e "s#%h/.local/bin/#$$tmp/#g" "$$unit" > "$$out"; \
		cp -f /bin/true "$$tmp/mcremote"; cp -f /bin/true "$$tmp/mcrelay"; \
		echo "  $$unit"; \
		systemd-analyze verify "$$out" || rc=1; \
	done; \
	exit $$rc

# Build the release Android APK locally (arm64) for on-device testing. Debug-
# signed unless apps/mobile/android/key.properties is present; the signed,
# published release APK is produced by CI on a version tag.
# Output: apps/mobile/build/app/outputs/flutter-apk/app-release.apk
# After build, scripts/assert-flutter-release-apk.sh verifies Flutter release mode.
apk:
	cd $(MOBILE_DIR) && flutter build apk --release --target-platform android-arm64
	@GRADLE_METADATA="$(MOBILE_DIR)/build/app/outputs/apk/release/output-metadata.json"; \
	  if [ ! -f "$$GRADLE_METADATA" ]; then unset GRADLE_METADATA; fi; \
	  GRADLE_METADATA="$${GRADLE_METADATA:-}" ./scripts/assert-flutter-release-apk.sh \
	    $(MOBILE_DIR)/build/app/outputs/flutter-apk/app-release.apk

# ---------------------------------------------------------------------------
# Flutter profile mode (runtime performance; not binary size).
# Docs: docs/mobile-profiling.md
# ---------------------------------------------------------------------------

# List connected devices (pick an Android id for DEVICE=).
profile-devices:
	@flutter devices

# Run the phone app in profile mode (near-release AOT + DevTools attach).
# Requires a physical Android device or emulator. Optional: DEVICE=<id>
# Example: make profile DEVICE=R5CNxxxxxxxx
profile:
	@set -e; \
	cd $(MOBILE_DIR); \
	flutter pub get; \
	if [ -n "$(DEVICE)" ]; then \
		echo "Profile run → device $(DEVICE)"; \
		flutter run --profile -d "$(DEVICE)"; \
	else \
		echo "Profile run → default device (set DEVICE=… to pick one; make profile-devices)"; \
		flutter run --profile; \
	fi

# Build a profile-mode arm64 APK (sideload / cold-start checks). Prefer
# `make profile` when you need DevTools. Output: app-profile.apk
# apps/mobile/build/app/outputs/flutter-apk/app-profile.apk
profile-apk:
	cd $(MOBILE_DIR) && flutter build apk --profile --target-platform android-arm64
	@echo "Profile APK: $(MOBILE_DIR)/build/app/outputs/flutter-apk/app-profile.apk"
	@ls -lh $(MOBILE_DIR)/build/app/outputs/flutter-apk/app-profile.apk 2>/dev/null || true

# install-hooks installs the repo's pre-commit hook AND makes sure Git will
# actually run it. A machine-wide core.hooksPath makes Git ignore .git/hooks
# entirely, which silently disables everything installed here — so when one is
# set, a chain shim goes into that directory to delegate back to this repo.
install-hooks:
	@echo "Installing git pre-commit hook..."
	@# Symlink, not copy: a copy goes stale the moment scripts/pre-commit.sh is
	@# edited, and nothing says so — the hook keeps running the old checks while
	@# the source shows the new ones. (That is how an unformatted Dart file
	@# reached CI after the check for it was written.)
	@ln -sfn ../../scripts/pre-commit.sh .git/hooks/pre-commit
	@chmod +x scripts/pre-commit.sh
	@chmod +x scripts/pre-commit.sh scripts/go-precheck.sh scripts/git-hooks-chain.sh
	@set -e; \
	HP="$$(git config --get core.hooksPath || true)"; \
	if [ -z "$$HP" ]; then \
		echo "Git pre-commit hook installed (.git/hooks)."; \
	else \
		ABS="$$(cd "$$HP" 2>/dev/null && pwd || echo "$$HP")"; \
		MINE="$$(cd .git/hooks && pwd)"; \
		if [ "$$ABS" = "$$MINE" ]; then \
			echo "Git pre-commit hook installed (core.hooksPath -> .git/hooks)."; \
		elif [ -w "$$ABS" ]; then \
			cp scripts/git-hooks-chain.sh "$$ABS/pre-commit"; \
			chmod +x "$$ABS/pre-commit"; \
			echo "core.hooksPath is $$ABS: installed a chain shim there so this repo's hook runs."; \
		else \
			echo "WARNING: core.hooksPath is $$ABS and is not writable."; \
			echo "  .git/hooks/pre-commit will NOT run. Fix with either:"; \
			echo "    cp scripts/git-hooks-chain.sh $$ABS/pre-commit && chmod +x $$ABS/pre-commit"; \
			echo "    git config --local core.hooksPath .git/hooks"; \
			exit 1; \
		fi; \
	fi
	@$(MAKE) --no-print-directory verify-hooks

# verify-hooks proves the hook is reachable rather than assuming it: it runs Git's
# own hook resolution and checks something executable answers for pre-commit.
verify-hooks:
	@set -e; \
	HP="$$(git config --get core.hooksPath || echo .git/hooks)"; \
	if [ ! -x "$$HP/pre-commit" ]; then \
		echo "verify-hooks: FAILED — no executable pre-commit at $$HP"; \
		echo "  run 'make install-hooks'"; \
		exit 1; \
	fi; \
	echo "verify-hooks: pre-commit reachable via $$HP"; \
	if [ -f .git/hooks/pre-commit ] && [ ! -L .git/hooks/pre-commit ] && \
	   ! cmp -s .git/hooks/pre-commit scripts/pre-commit.sh; then \
		echo "verify-hooks: FAILED — .git/hooks/pre-commit is a stale COPY of"; \
		echo "  scripts/pre-commit.sh. It is running checks that no longer match"; \
		echo "  the source. Run 'make install-hooks' (it now symlinks)."; \
		exit 1; \
	fi

run:
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Running mcremote $$VER…"; \
	CGO_ENABLED=$(CGO_ENABLED) go run \
		-ldflags "-X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		./cmd/mcremote serve

fmt:
	gofmt -w cmd internal

lint:
	golint ./cmd/... ./internal/...

vulncheck:
	govulncheck ./...

# The pre-add rule (AGENTS.md): gofmt, golint and govulncheck must be clean
# before Go files are staged. One script implements it, shared with the git
# pre-commit hook and the agent hook that gates `git add`, so they cannot drift.
# Pass FILES=... to check specific files instead of every tracked Go file.
FILES ?=
pre-add-check:
	@./scripts/go-precheck.sh $(FILES)

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

