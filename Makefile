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
# DATE is the commit timestamp, not wall-clock time, so the same source builds
# byte-identically every time (MADR 0060 D3). A wall-clock stamp made every
# build unique, which churns the binary's code identity — on macOS that means
# firewall and TCC decisions keyed to the old CDHash are re-prompted after each
# `make install`. SOURCE_DATE_EPOCH wins when set, for reproducible-build
# tooling; a tree with no git falls back to wall-clock.
ifdef SOURCE_DATE_EPOCH
  DATE  ?= $(shell date -u -r $(SOURCE_DATE_EPOCH) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d @$(SOURCE_DATE_EPOCH) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)
else
  DATE  ?= $(shell TZ=UTC git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
endif

# Release-grade build settings — shared by build, build-remote, build-relay so
# every mcremote/mcrelay artifact is identical in flags for a given GOOS.
#   CGO_ENABLED=0        → pure-Go binary (no cgo). On Linux this is fully
#                          static with netgo,osusergo. On Darwin, CGO=0 still
#                          uses the native DNS resolver and Directory Services
#                          user lookup via pure-Go syscalls (MADR 0059 D9) —
#                          do NOT force netgo/osusergo on Darwin.
#   -tags (Linux only)   → netgo,osusergo pin pure-Go DNS and passwd user
#                          lookup for static Linux deployment.
#   -trimpath            → strip absolute build paths → reproducible builds.
#   -ldflags -s -w       → drop the symbol table and DWARF debug info (smaller
#                          binary; panics still carry full stack traces).
# Override for a cgo build: make build CGO_ENABLED=1.
CGO_ENABLED   ?= 0
# Tags are computed after GOOS is known (see below).
GO_LDFLAGS    := -s -w

# True when the user passed VERSION=... on the command line.
VERSION_FROM_CLI := $(filter command line,$(origin VERSION))

# Host OS/arch → GOOS/GOARCH and user bin dir (override with GOOS=, GOARCH=, USER_BIN_DIR=).
UNAME_S := $(shell uname -s 2>/dev/null || echo unknown)
UNAME_M := $(shell uname -m 2>/dev/null || echo unknown)

# HOST_GOOS/HOST_GOARCH record what THIS machine can execute. They are set
# unconditionally (:=), unlike GOOS/GOARCH which are ?= defaults the caller may
# override to cross-compile. `install` compares the two and refuses to write a
# binary the host cannot run (MADR 0060 D8).
ifeq ($(UNAME_S),Linux)
  HOST_GOOS := linux
  GOOS   ?= linux
  # XDG user executables: ~/.local/bin
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifeq ($(UNAME_S),Darwin)
  HOST_GOOS := darwin
  GOOS   ?= darwin
  # Match Linux/XDG and setup-service default (~/.local/bin/mcremote).
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring MINGW,$(UNAME_S)))
  HOST_GOOS := windows
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring MSYS,$(UNAME_S)))
  HOST_GOOS := windows
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else ifneq (,$(findstring CYGWIN,$(UNAME_S)))
  HOST_GOOS := windows
  GOOS   ?= windows
  USER_BIN_DIR ?= $(HOME)/.local/bin
else
  HOST_GOOS := $(shell go env GOOS 2>/dev/null || echo linux)
  GOOS   ?= $(shell go env GOOS 2>/dev/null || echo linux)
  USER_BIN_DIR ?= $(HOME)/.local/bin
endif

# Platform-specific build tags (after GOOS is resolved; may be overridden by
# the environment when cross-compiling: make build GOOS=darwin GOARCH=arm64).
ifeq ($(GOOS),darwin)
  GO_TAGS :=
  GO_BUILDFLAGS := -trimpath
else ifeq ($(GOOS),linux)
  GO_TAGS := netgo,osusergo
  GO_BUILDFLAGS := -trimpath -tags $(GO_TAGS)
else
  # Other targets: explicit pure-Go net/user when static-friendly.
  GO_TAGS := netgo,osusergo
  GO_BUILDFLAGS := -trimpath -tags $(GO_TAGS)
endif

ifeq ($(UNAME_M),x86_64)
  HOST_GOARCH := amd64
  GOARCH ?= amd64
else ifeq ($(UNAME_M),amd64)
  HOST_GOARCH := amd64
  GOARCH ?= amd64
else ifeq ($(UNAME_M),aarch64)
  HOST_GOARCH := arm64
  GOARCH ?= arm64
else ifeq ($(UNAME_M),arm64)
  HOST_GOARCH := arm64
  GOARCH ?= arm64
else ifeq ($(UNAME_M),armv7l)
  HOST_GOARCH := arm
  GOARCH ?= arm
else ifeq ($(UNAME_M),i386)
  HOST_GOARCH := 386
  GOARCH ?= 386
else ifeq ($(UNAME_M),i686)
  HOST_GOARCH := 386
  GOARCH ?= 386
else
  HOST_GOARCH := $(shell go env GOARCH 2>/dev/null || echo amd64)
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

.PHONY: build debug build-relay build-remote install install-relay test live-opencode live-goose live-codex live-grok live-kilo race test-all preflight apk \
	verify-units verify-build-metadata profile profile-apk profile-devices run fmt lint staticcheck vulncheck \
	pre-add-check vet tidy clean check-host-target

build:
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH), cgo=$(CGO_ENABLED), tags=$(GO_TAGS))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN) ./cmd/mcremote; \
	echo "Building mcrelay $${VER}…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN_RELAY) ./cmd/mcrelay; \
	$(MAKE) --no-print-directory codesign-maybe

# Debug-tier build (MADR 0068 P6): pprof endpoints + the Go 1.26
# goroutine-leak profile, enabled at runtime only via MC_DEBUG_ADDR
# (loopback-only). Never ship these binaries — the tag compiles in a
# profiling surface release builds deliberately do not have. Keeps -ldflags
# symbol stripping off so profiles resolve names (docs/ops-mcrelay.md §6).
debug:
	@mkdir -p bin
	@echo "Building DEBUG mcremote + mcrelay (tags=debugpprof, GOEXPERIMENT=goroutineleakprofile)…"
	@GOEXPERIMENT=goroutineleakprofile CGO_ENABLED=$(CGO_ENABLED) go build -tags debugpprof \
		-ldflags "-X main.version=debug -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN) ./cmd/mcremote
	@GOEXPERIMENT=goroutineleakprofile CGO_ENABLED=$(CGO_ENABLED) go build -tags debugpprof \
		-ldflags "-X main.version=debug -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		-o $(BIN_RELAY) ./cmd/mcrelay

# Optional macOS code signing (MADR 0069 D6). With MC_CODESIGN_IDENTITY set
# to a certificate identity (list: `security find-identity -v -p codesigning`;
# a free Apple Development cert suffices), binaries get a stable identifier
# and an anchor-based designated requirement, so TCC grants (Full Disk
# Access, firewall) survive rebuilds and updates. Unset: the Go linker's
# ad-hoc signature stands — identity churns with every real code change and
# grants must be re-added after upgrades (docs/ops-macos-tcc.md).
MC_CODESIGN_IDENTITY ?=
codesign-maybe:
	@if [ -n "$(MC_CODESIGN_IDENTITY)" ] && [ "$(GOOS)" = "darwin" ]; then \
		echo "Signing with '$(MC_CODESIGN_IDENTITY)'…"; \
		codesign -f -s "$(MC_CODESIGN_IDENTITY)" -i com.magiccliremote.mcremote $(BIN); \
		codesign -f -s "$(MC_CODESIGN_IDENTITY)" -i com.magiccliremote.mcrelay $(BIN_RELAY); \
		codesign -d -r- $(BIN) 2>&1 | head -1; \
	fi

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
# and restores the service from a trap on every exit path including Ctrl-C. It
# also starts an enabled-but-stopped service, so a service left dead by an
# earlier failed install heals on the next `make install`.
#
# Service names passed to install-binary.sh are bare products (mcremote /
# mcrelay). On Linux that is the systemd --user unit; on macOS it maps to
# LaunchAgent labels com.magiccliremote.mcremote / com.magiccliremote.mcrelay
# (user domain only — no sudo).
# Refuse to install a binary this machine cannot execute. `install` honours
# GOOS/GOARCH through `build`, so `make install GOOS=linux` on a Mac would
# otherwise overwrite a working install with an unrunnable ELF (MADR 0060 D8).
#
# The guard is a prerequisite and the build is a recursive call, NOT a second
# prerequisite: make may run prerequisites concurrently under -j, so
# `install: check-host-target build` would let the compile start alongside the
# guard instead of after it.
check-host-target:
	@if [ "$(GOOS)" != "$(HOST_GOOS)" ] || [ "$(GOARCH)" != "$(HOST_GOARCH)" ]; then \
		echo "refusing to install $(GOOS)/$(GOARCH) on $(HOST_GOOS)/$(HOST_GOARCH)." >&2; \
		echo "cross-compile without installing: make build GOOS=$(GOOS) GOARCH=$(GOARCH)" >&2; \
		exit 1; \
	fi

install: check-host-target
	@$(MAKE) build
	@mkdir -p "$(USER_BIN_DIR)"
	@./scripts/install-binary.sh "$(BIN)" "$(INSTALL_PATH)" "$(SERVICE_NAME)"
	@"$(BIN)" version
	@./scripts/install-binary.sh "$(BIN_RELAY)" "$(USER_BIN_DIR)/mcrelay$(BIN_EXT)" \
		"$(RELAY_SERVICE_NAME)"
	@"$(BIN_RELAY)" version
	@case ":$$PATH:" in \
		*":$(USER_BIN_DIR):"*) ;; \
		*) echo "note: $(USER_BIN_DIR) is not on PATH; add it to run mcremote by name" ;; \
	esac

# Install mcrelay next to mcremote (does not stop/start a unit by default).
install-relay: check-host-target
	@$(MAKE) build-relay
	@mkdir -p "$(USER_BIN_DIR)"
	@./scripts/install-binary.sh "$(BIN_RELAY)" "$(USER_BIN_DIR)/mcrelay$(BIN_EXT)"
	@"$(BIN_RELAY)" version
	@case ":$$PATH:" in \
		*":$(USER_BIN_DIR):"*) ;; \
		*) echo "note: $(USER_BIN_DIR) is not on PATH; add it to run mcrelay by name" ;; \
	esac

test:
	go test ./...

# Live OpenCode HTTP suite (MADR 0020 Sprint 4 / A6). Requires `opencode` on
# PATH and network access to whatever model the engine uses (often free Zen).
# Not part of default `make test` / CI — best-effort; subagent/todo cases skip
# when the model does not cooperate. Timeout covers cold start + multi-turn.
live-opencode:
	go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 600s -v

# Live codex app-server suite (MADR 0044). Requires `codex` on PATH. Pins the
# thread/start vs turn/start wire shapes in both directions — the wrong shape
# must be *rejected* — which is the guard a fake cannot provide.
live-codex:
	go test -tags live_codex ./internal/provider/codex/ -count=1 -timeout 600s -v

# Token-bearing Codex turn suite (MADR 0080 P9). Explicitly opt-in.
live-codex-turn:
	go test -tags live_codex_turn ./internal/provider/codex/ -count=1 -timeout 600s -v

# Token-bearing inline review (MADR 0080 D19). Explicitly opt-in.
live-codex-review:
	go test -tags live_codex_review ./internal/provider/codex/ -count=1 -timeout 180s -v

# Live grok auth suite (MADR 0074 D22/D29, 0107). Requires `grok` on PATH.
# Pins the effective GROK_HOME resolution and the sibling auth.json.lock that
# grok's own writer honours, and proves an isolated device flow leaves the
# operator's real credential byte-identical. It cancels rather than completing
# an authorization, so it spends no tokens and mutates no real credential.
live-grok:
	go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 600s -v

# Live Kilo HTTP suite (MADR 0075/0088/0096/0108). Requires `kilo` on PATH.
# The aggregate catalog test requires a non-empty catalog; dedicated catalog
# cases may skip when their account/model prerequisites are absent. Prompt and
# tool checks are model-dependent. The 0108 surface/ACP/permission gates need
# no model. Set MCREMOTE_LIVE_KILO_MODEL to choose the turn model.
live-kilo:
	go test -tags live_kilo ./internal/provider/kilo/ -count=1 -timeout 600s -v

# Live goose ACP suite. Requires `goose` on PATH. Pins the contract probed
# on the installed version — including whether it advertises the UNSTABLE
# `session/delete` that MADR 0095 D10's purge is gated on.
live-goose:
	go test -tags live_goose ./internal/provider/goose/ -count=1 -timeout 600s -v

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
	@echo "==> staticcheck";   staticcheck ./...
	@echo "==> go test -race"; go test -race ./...
	@echo "==> version allocator tests"; ./scripts/next-build-version_test.sh
	@echo "==> install/restart tests"; ./scripts/install-binary_test.sh
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
	@if ! command -v systemd-analyze >/dev/null 2>&1; then \
		echo "verify-units: skipped (systemd-analyze not installed — Linux-only check)"; \
		exit 0; \
	fi; \
	set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	rc=0; \
	for unit in deploy/systemd/*.service; do \
		out="$$tmp/$$(basename $$unit)"; \
		sed -e "s#/usr/local/bin/#$$tmp/#g" -e "s#%h/.local/bin/#$$tmp/#g" "$$unit" > "$$out"; \
		printf '#!/bin/sh\nexit 0\n' > "$$tmp/mcremote"; \
		printf '#!/bin/sh\nexit 0\n' > "$$tmp/mcrelay"; \
		chmod +x "$$tmp/mcremote" "$$tmp/mcrelay"; \
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

run:
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$$( $(NEXT_VERSION_SH) "$(BASE_VERSION)" "$(BUILD_COUNTER_FILE)" )"; \
	fi; \
	echo "Running mcremote $${VER}…"; \
	CGO_ENABLED=$(CGO_ENABLED) go run \
		-ldflags "-X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE)" \
		./cmd/mcremote serve

fmt:
	gofmt -w cmd internal

lint:
	golint ./cmd/... ./internal/...

staticcheck:
	staticcheck ./...

vulncheck:
	govulncheck ./...

# The pre-add rule (AGENTS.md): gofmt, golint and govulncheck must be clean
# before Go files are staged. This script is the one implementation; the agent
# gate that blocks `git add` shells out to this very file, so the manual check
# and the enforced one cannot drift.
# Pass FILES=... to check specific files instead of every tracked Go file.
FILES ?=
pre-add-check:
	@./scripts/go-precheck.sh $(FILES)

# Assert release binaries carry the expected build tags (MADR 0059 D9).
# Builds temporary Darwin (no tags) and Linux (netgo,osusergo) artifacts.
verify-build-metadata:
	@./scripts/verify-build-metadata.sh

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

