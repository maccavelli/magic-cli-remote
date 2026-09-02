MODULE  := github.com/maccavelli/magic-cli-remote
# Version stamping (MADR 0005).
#   A release is an immutable strict tag: the tag build passes it directly as
#   VERSION= and stamps BUILD_KIND=release. There is no BASE.N build serial and
#   no build/<BASE>.N ledger tag any more — a rebuilt fix gets a new patch tag.
#   A local build stamps <base>.g<commit> and BUILD_KIND=local, which the
#   updater refuses to replace without --force.
# Override: make build VERSION=1.2.3
BASE_VERSION ?= $(shell git tag -l 'v*.*.*' 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -1 | sed 's/^v//' || echo 0.0.0)
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
# Shipped binaries are pure-Go and this is not overridable: the release
# targets refuse a non-zero value (see check-cgo-off). For a cgo build use
# `make debug`, which is never published (MADR 0116 D21).
CGO_ENABLED   ?= 0
# Tags are computed after GOOS is known (see below).
GO_LDFLAGS    := -s -w

# LOCAL_VERSION is the identity of a build that is not a published release.
# The .g<commit> suffix is the historical local shape and is what
# internal/updateclient normalizes; BUILD_KIND is what actually decides, since
# a bool cannot be set with the linker's -X flag.
LOCAL_VERSION := $(BASE_VERSION).g$(COMMIT)
BUILD_KIND ?= local

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
else ifeq ($(GOOS),windows)
  # No netgo/osusergo (MADR 0116 D2). osusergo is inert on Windows — os/user
  # has always been cgo-free there. netgo is NOT inert: it forces the pure-Go
  # resolver, overriding net/conf.go's goosPrefersCgo(), which names windows
  # explicitly. The pure-Go path reads DNS servers only from up-and-gatewayed
  # adapters (net/dnsconfig_windows.go) and honours no search list or NRPT
  # policy, so a VPN or virtual adapter's resolver is silently dropped.
  GO_TAGS :=
  GO_BUILDFLAGS := -trimpath
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

.PHONY: build debug build-relay build-remote install install-relay test live-opencode live-goose live-codex live-codex-contract live-grok live-kilo race test-all preflight apk manifest-surface \
	verify-units verify-build-metadata profile profile-apk profile-devices run fmt lint staticcheck vulncheck \
	pre-add-check vet tidy clean check-host-target check-cgo-off

build: check-cgo-off
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$(LOCAL_VERSION)"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH), cgo=$(CGO_ENABLED), tags=$(GO_TAGS))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildKind=$(BUILD_KIND)" \
		-o $(BIN) ./cmd/mcremote; \
	echo "Building mcrelay $${VER}…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildKind=$(BUILD_KIND)" \
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

build-relay: check-cgo-off
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$(LOCAL_VERSION)"; \
	fi; \
	echo "Building mcrelay $$VER ($(GOOS)/$(GOARCH))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildKind=$(BUILD_KIND)" \
		-o $(BIN_RELAY) ./cmd/mcrelay

# Build ONLY mcremote for one GOOS/GOARCH (mirror of build-relay). Lets the
# release job cross-compile each daemon over its own platform matrix, since
# mcremote and mcrelay ship different target sets. Pass VERSION= to reuse an
# already-allocated serial without touching the build/* ledger.
build-remote: check-cgo-off
	@mkdir -p bin
	@set -e; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then \
		VER="$(VERSION)"; \
	else \
		VER="$(LOCAL_VERSION)"; \
	fi; \
	echo "Building mcremote $$VER ($(GOOS)/$(GOARCH))…"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILDFLAGS) \
		-ldflags "$(GO_LDFLAGS) -X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildKind=$(BUILD_KIND)" \
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
# MADR 0116 D21. `CGO_ENABLED ?= 0` is a default the environment overrides:
#   $ CGO_ENABLED=1 make build   ->  make sees 1, and a cgo binary ships.
# Fail loudly rather than `override`-ing it silently — an operator who asked
# for cgo must be told they cannot have it here, not quietly ignored.
check-cgo-off:
	@if [ "$(CGO_ENABLED)" != "0" ]; then \
		echo "refusing to build a release binary with CGO_ENABLED=$(CGO_ENABLED)." >&2; \
		echo "Shipped binaries are pure-Go (MADR 0116 D21). For a cgo build," >&2; \
		echo "use 'make debug', which is never published." >&2; \
		exit 1; \
	fi

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

# Exact-binary Codex schema and no-model catalog drift gate (MADR 0109 P1).
live-codex-contract:
	go test -tags live_codex_contract ./internal/provider/codex/ -count=1 -timeout 180s -v

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
	@echo "==> install/restart tests"; ./scripts/install-binary_test.sh
	@echo "==> systemd units"; \
	if command -v systemd-analyze >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory verify-units; \
	else \
		echo "(skipped: systemd-analyze not installed)"; \
	fi
	@echo "==> release build (mcremote + mcrelay)"; \
	$(MAKE) --no-print-directory build >/dev/null
	@./bin/mcremote version
	@./bin/mcrelay version
	@echo "==> flutter pin"; ./scripts/assert-flutter-pin.sh
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

# Check the shipped Android permission / exported-component surface against
# apps/mobile/android/manifest-surface.allow (MADR 0126 D4). Nothing else in
# this repo reads the merged manifest, which is how a plugin injected WAKE_LOCK,
# RECEIVE_BOOT_COMPLETED, VIBRATE and an exported receiver unnoticed (0126 F3).
manifest-surface:
	cd $(MOBILE_DIR) && flutter build apk --config-only --release --target-platform android-arm64
	cd $(MOBILE_DIR)/android && ./gradlew :app:processReleaseManifest -q
	./scripts/assert-android-manifest-surface.sh

# Build the release Android APK locally (arm64) for on-device testing. Debug-
# signed unless apps/mobile/android/key.properties is present; the signed,
# published release APK is produced by CI on a version tag.
# Output: apps/mobile/build/app/outputs/flutter-apk/app-release.apk
# After build, scripts/assert-flutter-release-apk.sh verifies Flutter release mode.
# MADR 0126 F8: stamp the version. Without --build-name/--build-number the APK
# takes pubspec.yaml's placeholder (0.1.0 / 1), so AppUpdateService compares the
# release tag against "0.1.0" and reports an update for ever — breaking the one
# workflow that needs a local APK, namely testing the updater. CI already does
# this (ci.yml); this target simply never did.
#
# MCREMOTE_VERSION_PUSH=0 MCREMOTE_VERSION_TAG=0 is the same pair `preflight`
# uses: a developer's local build must not claim a serial from the shared ledger
# or push a build/* tag. Falls back to an unstamped build if the allocator
# cannot produce a well-formed version.
#
# BUILD_NAME is the FULL four-part version, matching CI (ci.yml passes
# needs.go.outputs.version) and the intent stated there — "versionName is the
# full Go build version so the APK and the binaries in the same release agree".
# The first version of this target split it to three parts, copying
# scripts/build-apk.sh; that made a local APK's versionName differ in shape from
# a CI one for no reason (MADR 0128 D2). --build-number stays the serial
# locally and github.run_number in CI: that difference IS deliberate, because a
# versionCode must increase monotonically forever while N restarts at 1 on each
# new release base.
apk:
	@set -e; \
	VER="$(LOCAL_VERSION)"; \
	BUILD_NAME="$$VER"; BUILD_NUMBER="$${BUILD_NUMBER:-1}"; \
	if [ -n "$(VERSION_FROM_CLI)" ]; then BUILD_NAME="$(VERSION)"; fi; \
	if echo "$$BUILD_NAME" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(\.[0-9]+)?$$' && \
	   echo "$$BUILD_NUMBER" | grep -qE '^[0-9]+$$'; then \
		echo "==> apk $$BUILD_NAME ($$BUILD_NUMBER)"; \
		cd $(MOBILE_DIR) && flutter build apk --release --target-platform android-arm64 \
			--build-name="$$BUILD_NAME" --build-number="$$BUILD_NUMBER"; \
	else \
		echo "warning: no well-formed build version ($$VER); building unstamped" >&2; \
		cd $(MOBILE_DIR) && flutter build apk --release --target-platform android-arm64; \
	fi
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
		VER="$(LOCAL_VERSION)"; \
	fi; \
	echo "Running mcremote $${VER}…"; \
	CGO_ENABLED=$(CGO_ENABLED) go run \
		-ldflags "-X main.version=$$VER -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildKind=$(BUILD_KIND)" \
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

# Dart production files created under MADR 0112 A13, which requires each to hold
# at least 90.0% line coverage. They are recorded here rather than only in a
# phase's own command so that 0113's `make coverage-check` target can consume
# one list instead of rediscovering it from the plans.
#
# Enforce with:
#   scripts/coverage-delta.sh floor --after DIR --minimum 80.0 \
#     --dart-root apps/mobile $(NEW_DART_FILES:%=--new-dart-file %)
NEW_DART_FILES := \
	lib/features/chat/workspace_sheet.dart \
	lib/features/chat/diagnostics_sheet.dart \
	lib/features/chat/skill_authoring_sheet.dart \
	lib/features/chat/session_share_sheet.dart \
	lib/features/chat/shell_command_sheet.dart \
	lib/features/chat/session_controls/session_control_card.dart \
	lib/features/chat/session_controls/session_control_cards.dart \
	lib/features/chat/session_controls/composer_actions_row.dart \
	lib/features/chat/session_controls/control_glyphs.dart \
	lib/features/chat/session_controls/ui_icons.dart

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
