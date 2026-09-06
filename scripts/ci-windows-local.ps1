#Requires -Version 5.1
<#
.SYNOPSIS
    Local CI-style Windows gates for MAC420 (MADR/PLAN 0145).

.DESCRIPTION
    Windows-host-only. Mirrors CI go-native windows/amd64 unit contract by
    default; -Smoke covers light release-shaped version stamps.
    Functional F5/paths/doctor stay on scripts/acceptance-windows.ps1.

    Never register this laptop as a GHA self-hosted runner (MADR 0116 F20).
    On non-Windows hosts this script prints a clear skip line and exits 0
    (skip is not a silent PASS).

.PARAMETER UnitOnly
    Run checklist A only (default when no -Smoke).

.PARAMETER Smoke
    Run checklist B (ci-windows-smoke).

.PARAMETER RetryOnce
    Re-run go test once on failure (CI 0143 behaviour). NOT the Make default.

.PARAMETER SkipTests
    Skip go test only (document loudly). Build/vet still run for UnitOnly.
#>
[CmdletBinding()]
param(
    [switch]$UnitOnly,
    [switch]$Smoke,
    [switch]$RetryOnce,
    [switch]$SkipTests
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Host guard (PLAN A0/B0): skip+exit 0 off-Windows; message required (≠ PASS).
$isWin = $false
try {
    if ($IsWindows) { $isWin = $true }
} catch { }
if (-not $isWin -and $env:OS -match 'Windows') { $isWin = $true }
if (-not $isWin) {
    $osDesc = $env:OS
    try {
        $osDesc = [System.Runtime.InteropServices.RuntimeInformation]::OSDescription
    } catch { }
    Write-Host "Windows-only; skipping on $osDesc"
    exit 0
}

if (-not $Smoke -and -not $UnitOnly) { $UnitOnly = $true }
if ($Smoke -and $UnitOnly) {
    Write-Error "Use either -Smoke or unit mode, not both."
    exit 2
}

$script:Failures = 0
function Fail([string]$Msg) {
    Write-Host "FAIL  $Msg" -ForegroundColor Red
    $script:Failures++
}
function Pass([string]$Msg) {
    Write-Host "PASS  $Msg" -ForegroundColor Green
}

function Invoke-Checked {
    param([string]$Name, [scriptblock]$Body)
    Write-Host ""
    Write-Host "== $Name" -ForegroundColor Cyan
    try {
        & $Body
        if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
            throw "exit code $LASTEXITCODE"
        }
        Pass $Name
    } catch {
        Fail "$Name -- $_"
    }
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    Write-Host "ci-windows-local: Windows host; CGO_ENABLED=0; MC_REQUIRE_SYMLINK=1 (0145)"

    $env:CGO_ENABLED = '0'
    $env:MC_REQUIRE_SYMLINK = '1'

    if ($Smoke) {
        # --- Checklist B ---
        $ver = $env:VERSION
        if (-not $ver) {
            $base = '0.0.0'
            $tags = git tag -l 'v*.*.*' 2>$null
            if ($tags) {
                $pick = $tags | Where-Object { $_ -match '^v\d+\.\d+\.\d+$' } | Sort-Object { [version]($_ -replace '^v','') } | Select-Object -Last 1
                if ($pick) { $base = $pick.Substring(1) }
            }
            $commit = (git rev-parse --short HEAD 2>$null)
            if (-not $commit) { $commit = 'none' }
            $ver = "$base.g$commit"
        }
        $commit = (git rev-parse --short HEAD 2>$null); if (-not $commit) { $commit = 'none' }
        $date = (git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>$null)
        if (-not $date) { $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ') }
        $ld = "-s -w -X main.version=$ver -X main.commit=$commit -X main.date=$date -X main.buildKind=local"

        New-Item -ItemType Directory -Force -Path (Join-Path $root 'dist') | Out-Null
        $remoteOut = Join-Path $root 'dist\mcremote-windows-amd64.exe'
        $relayOut = Join-Path $root 'dist\mcrelay-windows-amd64.exe'

        Invoke-Checked 'B12 build dist/*-windows-amd64.exe' {
            $env:CGO_ENABLED = '0'
            go build -trimpath -ldflags $ld -o $remoteOut ./cmd/mcremote
            if ($LASTEXITCODE -ne 0) { throw "mcremote build exit $LASTEXITCODE" }
            go build -trimpath -ldflags $ld -o $relayOut ./cmd/mcrelay
            if ($LASTEXITCODE -ne 0) { throw "mcrelay build exit $LASTEXITCODE" }
        }

        Invoke-Checked 'B13 go version -m CGO_ENABLED=0' {
            foreach ($b in @($remoteOut, $relayOut)) {
                $meta = go version -m $b | Out-String
                if ($meta -notmatch 'CGO_ENABLED=0') { throw "$b missing CGO_ENABLED=0" }
            }
        }

        Invoke-Checked 'B14 version identity' {
            foreach ($b in @($remoteOut, $relayOut)) {
                $out = & $b version 2>&1 | Out-String
                if ($LASTEXITCODE -ne 0) { throw "$b version exit $LASTEXITCODE" }
                if ($out -notmatch [regex]::Escape($ver)) {
                    throw "$b version output did not contain stamped VERSION=$ver`n$out"
                }
            }
        }
    } else {
        # --- Checklist A ---
        Invoke-Checked 'A3 go.mod toolchain' {
            $modLine = Get-Content (Join-Path $root 'go.mod') | Where-Object { $_ -match '^go\s+' } | Select-Object -First 1
            if (-not $modLine) { throw 'no go directive in go.mod' }
            if ($modLine -notmatch '^go\s+(\d+\.\d+(?:\.\d+)?)') { throw "unparseable go.mod: $modLine" }
            $need = $Matches[1]
            $gv = go version
            if ($gv -notmatch 'go(\d+\.\d+(?:\.\d+)?)') { throw "unparseable go version: $gv" }
            $have = $Matches[1]
            $needV = [version]($need + $(if ($need.Split('.').Count -lt 3) { '.0' } else { '' }))
            $haveParts = $have.Split('.')
            while ($haveParts.Count -lt 3) { $haveParts += '0' }
            $haveV = [version](($haveParts[0..2] -join '.'))
            $needParts = $need.Split('.')
            while ($needParts.Count -lt 3) { $needParts += '0' }
            $needV = [version](($needParts[0..2] -join '.'))
            if ($haveV -lt $needV) {
                throw "go $have < go.mod requires $need (use go from go.mod, not Windows-preinstalled default)"
            }
        }

        Invoke-Checked 'A2 symlink probe (MC_REQUIRE_SYMLINK=1)' {
            $target = Join-Path $env:TEMP ('mc-symlink-target-' + [guid]::NewGuid().ToString('N'))
            $link = Join-Path $env:TEMP ('mc-symlink-link-' + [guid]::NewGuid().ToString('N'))
            New-Item -ItemType File -Path $target -Force | Out-Null
            try {
                New-Item -ItemType SymbolicLink -Path $link -Target $target -ErrorAction Stop | Out-Null
            } catch {
                throw "cannot create symlinks (SeCreateSymbolicLinkPrivilege?): $_"
            } finally {
                Remove-Item -LiteralPath $link, $target -Force -ErrorAction SilentlyContinue
            }
            if ($env:MC_REQUIRE_SYMLINK -ne '1') { throw 'MC_REQUIRE_SYMLINK not set to 1' }
            if ($env:CGO_ENABLED -ne '0') { throw 'CGO_ENABLED not 0' }
        }

        Invoke-Checked 'A4 go build ./...' {
            $env:CGO_ENABLED = '0'
            go build ./...
            if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE" }
        }
        Invoke-Checked 'A5 go vet ./...' {
            go vet ./...
            if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE" }
        }
        if ($SkipTests) {
            Write-Host 'WARN  A6 skipped (-SkipTests); not a full ci-windows green' -ForegroundColor Yellow
        } else {
            Invoke-Checked 'A6 go test ./... (no -race, no live_*)' {
                go test ./...
                if ($LASTEXITCODE -ne 0) {
                    if ($RetryOnce) {
                        Write-Host 'retry once (-RetryOnce)...' -ForegroundColor Yellow
                        go test ./...
                        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE after retry" }
                    } else {
                        throw "exit $LASTEXITCODE"
                    }
                }
            }
        }
    }

    Write-Host ''
    if ($script:Failures -eq 0) {
        Write-Host 'ci-windows-local: ALL SELECTED CHECKS PASSED' -ForegroundColor Green
        exit 0
    }
    Write-Host "ci-windows-local: $($script:Failures) CHECK(S) FAILED" -ForegroundColor Red
    exit 1
} finally {
    Pop-Location
}
