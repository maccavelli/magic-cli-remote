#Requires -Version 5.1
<#
.SYNOPSIS
    Manual acceptance run for windows/amd64.

.DESCRIPTION
    Run this on the owner's Windows laptop. It builds, vets and tests the tree,
    then exercises the functional paths that only a real Windows host can
    prove: the resolved path layout, pairing (the direct MADR 0116 F5
    regression), and a graceful Ctrl+C drain.

    This laptop is NOT a CI runner and must never be registered as a
    self-hosted GitHub Actions runner: this repository is public, so anyone who
    can fork it and open a pull request could execute code on it, and
    self-hosted runners are non-ephemeral by default (MADR 0116 F20). CI for
    windows/amd64 is the hosted windows-latest job.

.PARAMETER SkipTests
    Skip the Go suite and run only the functional checks.
#>
[CmdletBinding()]
param([switch]$SkipTests)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Continue'

# Contract C7 (MADR 0116 D20): cgo-free, builds and tests. No -race here — off
# darwin it forces CGO_ENABLED=1, which C7 refuses.
$env:CGO_ENABLED = '0'

$script:Failures = 0

function Invoke-Check {
    param([string]$Name, [scriptblock]$Body)
    Write-Host ''
    Write-Host "== $Name" -ForegroundColor Cyan
    try {
        & $Body
        if ($LASTEXITCODE -ne 0 -and $null -ne $LASTEXITCODE) {
            throw "exit code $LASTEXITCODE"
        }
        Write-Host "PASS  $Name" -ForegroundColor Green
    } catch {
        Write-Host "FAIL  $Name -- $_" -ForegroundColor Red
        $script:Failures++
    }
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    Write-Host "acceptance: windows/amd64, CGO_ENABLED=$env:CGO_ENABLED"

    Invoke-Check 'go build ./...' { go build ./... }
    Invoke-Check 'go vet ./...' { go vet ./... }
    if (-not $SkipTests) {
        Invoke-Check 'go test ./...' { go test ./... }
    }

    $bin = Join-Path $root 'bin\mcremote.exe'
    Invoke-Check 'build binaries' {
        go build -o (Join-Path $root 'bin\mcremote.exe') ./cmd/mcremote
        go build -o (Join-Path $root 'bin\mcrelay.exe') ./cmd/mcrelay
    }

    Invoke-Check 'binaries are cgo-free (D21)' {
        foreach ($b in @('bin\mcremote.exe', 'bin\mcrelay.exe')) {
            $meta = go version -m (Join-Path $root $b)
            if (-not ($meta -match 'CGO_ENABLED=0')) {
                throw "$b is not CGO_ENABLED=0"
            }
        }
    }

    Invoke-Check 'mcremote version' { & $bin version }

    # Prints the resolved Known Folders layout (MADR 0116 D3). Eyeball it:
    # config under %AppData%, everything else under %LocalAppData%\mcremote,
    # and no "-1" anywhere (F4).
    Invoke-Check 'mcremote paths' { & $bin paths }

    # THE direct F5 regression: before D5, SyncDir returned "Access is denied"
    # on Windows and pairing reported failure on a write that had landed.
    $dataDir = Join-Path $env:TEMP ("mcaccept-" + [Guid]::NewGuid().ToString('N'))
    Invoke-Check 'mcremote pair create (F5 regression)' {
        & $bin pair create --name acceptance --data-dir $dataDir
    }
    Invoke-Check 'mcremote pair list' {
        & $bin pair list --data-dir $dataDir
    }
    Remove-Item -Path $dataDir -Recurse -Force -ErrorAction SilentlyContinue

    Invoke-Check 'mcremote doctor' { & $bin doctor }

    Write-Host ''
    if ($script:Failures -eq 0) {
        Write-Host 'acceptance: ALL CHECKS PASSED' -ForegroundColor Green
    } else {
        Write-Host "acceptance: $($script:Failures) CHECK(S) FAILED" -ForegroundColor Red
    }

    Write-Host ''
    Write-Host 'Manual steps this script cannot assert:'
    Write-Host '  1. mcrelay serve --listen-host 127.0.0.1 --listen-port 8443'
    Write-Host '     then Ctrl+C -- the drain must run (MADR 0116 D9).'
    Write-Host '  2. mcremote setup-service --force  from a NON-ADMINISTRATOR shell.'
    Write-Host '     If it prompts for elevation, D12 was implemented wrongly.'

    exit $script:Failures
} finally {
    Pop-Location
}
