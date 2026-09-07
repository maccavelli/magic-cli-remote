# 0145 local Windows CI gates (included from Makefile)
# HOST_GOOS is defined in the parent Makefile.

# The guard and the invocation MUST share one shell. Make runs each recipe line
# in its own shell, so an `exit 0` on a guard line ends only that line — the
# next line still runs. The first version of this file was written that way and
# printed the skip message and then invoked powershell anyway, so `make
# ci-windows` exited 2 on macOS/Linux instead of skipping (PLAN 0145 A0).
#
# Defined once as a variable rather than repeated per target: two copies of a
# guard is how enumerations drift out of sync here (AGENTS.md F2). A new
# Windows-only target is one line that reuses $(WIN_ONLY).
WIN_ONLY = if [ "$(HOST_GOOS)" != "windows" ]; then echo "Windows-only; skipping on $(HOST_GOOS) (use make preflight on unix)"; exit 0; fi

.PHONY: ci-windows ci-windows-smoke

ci-windows:
	@$(WIN_ONLY); powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1

ci-windows-smoke:
	@$(WIN_ONLY); powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1 -Smoke
