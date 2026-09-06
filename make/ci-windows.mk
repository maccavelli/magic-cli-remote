# 0145 local Windows CI gates (included from Makefile)
# HOST_GOOS is defined in the parent Makefile.

.PHONY: ci-windows ci-windows-smoke

ci-windows:
	@if [ "$(HOST_GOOS)" != "windows" ]; then \
	  echo "Windows-only; skipping on $(HOST_GOOS) (use make preflight on unix)"; \
	  exit 0; \
	fi
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1

ci-windows-smoke:
	@if [ "$(HOST_GOOS)" != "windows" ]; then \
	  echo "Windows-only; skipping on $(HOST_GOOS) (use make preflight on unix)"; \
	  exit 0; \
	fi
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1 -Smoke
