#Requires -Version 5.1
<#
.SYNOPSIS
    Manual acceptance run for windows/amd64.

.DESCRIPTION
    Run this on the owner's Windows laptop. It builds, vets and tests the tree,
    then exercises the functional paths that only a real Windows host can
    prove: the resolved path layout (hardened JSON asserts, PLAN 0145 C1),
    pairing (MADR 0116 F5), and doctor exit 0 (C3).

    This laptop is NOT a CI runner and must never be registered as a
    self-hosted GitHub Actions runner (MADR 0116 F20). Unit/CI-mirror gates
    live in scripts/ci-windows-local.ps1 (`make ci-windows`).

.PARAMETER SkipTests
    Skip the Go suite and run only the functional checks.
#>
[CmdletBinding()]
param([switch]$SkipTests)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Continue'

# Host guard: Windows-only; skip+exit 0 off-Windows (message ≠ PASS).
$isWin = $false
try { if ($IsWindows) { $isWin = $true } } catch { }
if (-not $isWin -and $env:OS -match 'Windows') { $isWin = $true }
if (-not $isWin) {
    $osDesc = $env:OS
    try { $osDesc = [System.Runtime.InteropServices.RuntimeInformation]::OSDescription } catch { }
    Write-Host "Windows-only; skipping on $osDesc"
    exit 0
}

# Contract C7 (MADR 0116 D20): cgo-free. No -race.
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

function Get-FullPathCI([string]$p) {
    return [IO.Path]::GetFullPath($p)
}

function Assert-SamePath([string]$Got, [string]$Want, [string]$Label) {
    $g = Get-FullPathCI $Got
    $w = Get-FullPathCI $Want
    if (-not $g.Equals($w, [StringComparison]::OrdinalIgnoreCase)) {
        throw "$Label: got '$g' want '$w'"
    }
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    Write-Host "acceptance: windows/amd64, CGO_ENABLED=$env:CGO_ENABLED (0145 C)"

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

    # C1: paths --json hardened asserts (PLAN 0145)
    Invoke-Check 'mcremote paths --json (C1)' {
        $raw = & $bin paths --json
        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE" }
        $j = $raw | ConvertFrom-Json

        $blob = ($raw | Out-String)
        if ($blob -match '(^|[\\/])-1([\\/]|$)' -or $blob -match 'mcremote-runtime--1') {
            throw 'forbidden -1 path segment / mcremote-runtime--1 present'
        }

        if ($j.product -ne 'mcremote') { throw "product=$($j.product)" }

        $appData = $env:APPDATA
        $local = $env:LOCALAPPDATA
        if (-not $appData -or -not $local) { throw 'APPDATA/LOCALAPPDATA unset' }

        $cfg = Get-FullPathCI $j.config_dir
        $appRoot = Get-FullPathCI (Join-Path $appData 'mcremote')
        if (-not $cfg.StartsWith($appRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "config_dir not under APPDATA\mcremote: $cfg"
        }
        $localRoot = Get-FullPathCI $local
        if ($cfg.StartsWith($localRoot, [StringComparison]::OrdinalIgnoreCase) -and
            $cfg.IndexOf('\Local\', [StringComparison]::OrdinalIgnoreCase) -ge 0) {
            # Allow only if somehow overlapping; require not under LOCALAPPDATA\mcremote
        }
        $localMc = Get-FullPathCI (Join-Path $local 'mcremote')
        if ($cfg.StartsWith($localMc, [StringComparison]::OrdinalIgnoreCase)) {
            throw "config_dir must not be under LOCALAPPDATA\mcremote: $cfg"
        }

        Assert-SamePath $j.data_dir (Join-Path $local 'mcremote') 'data_dir'
        Assert-SamePath $j.state_dir (Join-Path $local 'mcremote\State') 'state_dir'
        Assert-SamePath $j.cache_dir (Join-Path $local 'mcremote\Cache') 'cache_dir'

        if ($j.instance_key -notmatch '^[0-9a-f]{16}$') {
            throw "instance_key invalid: $($j.instance_key)"
        }
        if ($j.instance_key -eq '-1') { throw 'instance_key is -1' }

        $rtBase = Join-Path $local 'mcremote\Runtime'
        $wantRt = Join-Path $rtBase $j.instance_key
        Assert-SamePath $j.runtime_dir $wantRt 'runtime_dir'

        if ($j.PSObject.Properties.Name -contains 'log_dir' -and $j.log_dir) {
            Assert-SamePath $j.log_dir (Join-Path $local 'mcremote\Logs') 'log_dir'
        }

        Assert-SamePath $j.admin_socket (Join-Path $j.runtime_dir 'admin.sock') 'admin_socket'

        foreach ($field in @('config_dir','data_dir','state_dir','cache_dir','runtime_dir','admin_socket')) {
            $p = $j.$field
            if (-not [IO.Path]::IsPathRooted($p)) { throw "$field not absolute: $p" }
        }

        if (-not $j.engine_registry_dir) { throw 'engine_registry_dir empty' }
    }

    # C2 pair (F5)
    $dataDir = Join-Path $env:TEMP ("mcaccept-" + [Guid]::NewGuid().ToString('N'))
    Invoke-Check 'mcremote pair create (F5 / C2)' {
        & $bin pair create --name acceptance --data-dir $dataDir
    }
    Invoke-Check 'mcremote pair list (C2)' {
        $out = & $bin pair list --data-dir $dataDir | Out-String
        if ($out -notmatch 'acceptance') { throw "pair list missing name: $out" }
    }
    Remove-Item -Path $dataDir -Recurse -Force -ErrorAction SilentlyContinue

    # C3 doctor: exit 0 only (no healthy-service assert)
    Invoke-Check 'mcremote doctor exit 0 (C3)' {
        & $bin doctor
        if ($LASTEXITCODE -ne 0) { throw "doctor exit $LASTEXITCODE" }
    }

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
