#Requires -Version 5.1
<#
.SYNOPSIS
    Acceptance run for the Windows service lifecycle (MADR/PLAN 0159 D11, P11).

.DESCRIPTION
    Registers a real per-user Task Scheduler task, exercises its whole
    lifecycle, and removes it. It drives **mcrelay**, not mcremote, so the
    operator's live mcremote task is never touched (PLAN 0159 C3). It proves
    that task is byte-identical before and after.

    Checks, in order:
      S0  not elevated; no mcrelay task already registered
      S1  build mcrelay; a private temp config the relay's guard accepts
      S2  setup-service registers the task, with a Windows-only summary
      S3  the task reaches Running, from the temp binary
      S4  --refresh --json reports "unchanged"
      S5  setup-service again, without --force, succeeds (D14 idempotency)
      S6  a killed daemon is relaunched by the watchdog trigger (D5)
      S7  stop (disable + end) keeps it stopped
      S8  --remove leaves no task and no process
      C3  the live mcremote task definition is unchanged

    Stateful by design: it is NOT part of `make ci-windows`. It must run from a
    NON-elevated shell (MADR 0116 D12) and cleans up in a finally block.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Continue'

$isWin = $false
try { if ($IsWindows) { $isWin = $true } } catch { }
if (-not $isWin -and $env:OS -match 'Windows') { $isWin = $true }
if (-not $isWin) { Write-Host 'Windows-only; skipping'; exit 0 }

$script:Failures = 0
function Invoke-Check {
    param([string]$Name, [scriptblock]$Body)
    Write-Host ''
    Write-Host "== $Name" -ForegroundColor Cyan
    try {
        $global:LASTEXITCODE = 0
        & $Body
        Write-Host "PASS  $Name" -ForegroundColor Green
    } catch {
        Write-Host "FAIL  $Name -- $_" -ForegroundColor Red
        $script:Failures++
    }
}

function Get-TaskState([string]$name) {
    $t = Get-ScheduledTask -TaskPath '\' -TaskName $name -ErrorAction SilentlyContinue
    if ($t) { return [string]$t.State }
    return 'absent'
}

function Get-RelayProcess([string]$dir) {
    # The leading comma keeps an empty or single result an array: PowerShell
    # otherwise unrolls it to $null or a scalar, and StrictMode then rejects
    # .Count on $null.
    return , @(Get-CimInstance Win32_Process -Filter "Name='mcrelay.exe'" |
            Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($dir, [StringComparison]::OrdinalIgnoreCase) })
}

function Wait-Until([int]$Seconds, [scriptblock]$Condition) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        if (& $Condition) { return $true }
        Start-Sleep -Milliseconds 500
    }
    return [bool](& $Condition)
}

# ---- S0: preconditions ------------------------------------------------------
$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host 'FAIL  S0: running elevated; this gate must run unelevated (MADR 0116 D12)' -ForegroundColor Red
    exit 1
}
if ((Get-TaskState 'mcrelay') -ne 'absent') {
    Write-Host 'FAIL  S0: a mcrelay task is already registered; this script will not touch it' -ForegroundColor Red
    exit 1
}

$root = Split-Path -Parent $PSScriptRoot
$T = Join-Path $env:TEMP ('mcr-accept-' + [Guid]::NewGuid().ToString('N'))
$bin = Join-Path $T 'mcrelay.exe'
$cfgDir = Join-Path $T 'cfg'
$cfg = Join-Path $cfgDir 'config.yaml'
$relayLocal = Join-Path $env:LOCALAPPDATA 'mcrelay'
$relayRoaming = Join-Path $env:APPDATA 'mcrelay'
$hadRelayLocal = Test-Path $relayLocal
$hadRelayRoaming = Test-Path $relayRoaming

# C3 baseline: the operator's live mcremote task, exported before anything runs.
$mcremoteBefore = $null
if ((Get-TaskState 'mcremote') -ne 'absent') {
    $mcremoteBefore = (schtasks /query /tn mcremote /xml ONE) -join "`n"
}

Push-Location $root
try {
    Write-Host "service acceptance: temp dir $T"
    New-Item -ItemType Directory -Path $cfgDir -Force | Out-Null

    Invoke-Check 'S1 build mcrelay and a private config' {
        $env:CGO_ENABLED = '0'
        go build -o $bin ./cmd/mcrelay
        if ($LASTEXITCODE -ne 0) { throw "go build exit $LASTEXITCODE" }
        $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
        icacls $cfgDir /inheritance:r /grant:r "*${sid}:(OI)(CI)F" | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "icacls exit $LASTEXITCODE" }
        $l = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
        $l.Start(); $port = $l.LocalEndpoint.Port; $l.Stop()
        $yaml = "listen:`n  host: 127.0.0.1`n  port: $port`ntls:`n  mode: off`nhosts:`n  - id: acceptance`n    secret: acceptance-secret-0159`n"
        [IO.File]::WriteAllText($cfg, $yaml)
        & $bin paths --json --config $cfg | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "mcrelay paths refused the config (exit $LASTEXITCODE): not private?" }
    }

    Invoke-Check 'S2 setup-service registers the task (Windows summary only)' {
        $out = (& $bin setup-service --binary $bin --service-config $cfg --force 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE`n$out" }
        foreach ($bad in 'systemctl', 'journalctl', 'loginctl', '.service', 'make install') {
            if ($out.Contains($bad)) { throw "summary names '$bad':`n$out" }
        }
        if (-not $out.Contains('schtasks /query /tn mcrelay')) { throw "summary lacks the schtasks status command:`n$out" }
    }

    Invoke-Check 'S3 the task is Running from the temp binary' {
        $ok = Wait-Until 30 { (Get-TaskState 'mcrelay') -eq 'Running' -and (Get-RelayProcess $T).Count -eq 1 }
        if (-not $ok) { throw "state=$(Get-TaskState 'mcrelay') processes=$((Get-RelayProcess $T).Count)" }
    }

    Invoke-Check 'S4 --refresh --json reports unchanged' {
        $json = (& $bin setup-service --refresh --json 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE`n$json" }
        $r = $json | ConvertFrom-Json
        if ($r.verdict -ne 'unchanged') { throw "verdict=$($r.verdict) reason=$($r.reason)" }
    }

    Invoke-Check 'S5 setup-service again without --force is idempotent' {
        $before = (schtasks /query /tn mcrelay /xml ONE) -join "`n"
        $out = (& $bin setup-service --binary $bin --service-config $cfg 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE (a changed definition would need --force)`n$out" }
        $after = (schtasks /query /tn mcrelay /xml ONE) -join "`n"
        if ($before -ne $after) { throw 'the registered definition changed' }
    }

    Invoke-Check 'S6 a killed daemon is relaunched within 120 s' {
        $first = (Get-RelayProcess $T)[0].ProcessId
        Stop-Process -Id $first -Force
        $ok = Wait-Until 120 {
            $p = Get-RelayProcess $T
            $p.Count -eq 1 -and $p[0].ProcessId -ne $first -and (Get-TaskState 'mcrelay') -eq 'Running'
        }
        if (-not $ok) { throw "no relaunch: state=$(Get-TaskState 'mcrelay') processes=$((Get-RelayProcess $T).Count)" }
    }

    Invoke-Check 'S7 stop (disable + end) keeps it stopped for 90 s' {
        schtasks /change /tn mcrelay /disable | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "disable exit $LASTEXITCODE" }
        schtasks /end /tn mcrelay | Out-Null
        Start-Sleep -Seconds 90
        $n = (Get-RelayProcess $T).Count
        if ($n -ne 0) { throw "$n mcrelay process(es) running after a stop" }
        if ((Get-TaskState 'mcrelay') -ne 'Disabled') { throw "state=$(Get-TaskState 'mcrelay'), want Disabled" }
    }

    Invoke-Check 'S8 --remove leaves no task and no process' {
        & $bin setup-service --remove | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "exit $LASTEXITCODE" }
        if ((Get-TaskState 'mcrelay') -ne 'absent') { throw 'the task is still registered' }
        if ((Get-RelayProcess $T).Count -ne 0) { throw 'a mcrelay process survived --remove' }
    }
} finally {
    if ((Get-TaskState 'mcrelay') -ne 'absent') {
        schtasks /end /tn mcrelay 2>&1 | Out-Null
        Unregister-ScheduledTask -TaskName mcrelay -Confirm:$false -ErrorAction SilentlyContinue
    }
    Get-RelayProcess $T | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Pop-Location
    Start-Sleep -Milliseconds 500
    Remove-Item -Recurse -Force $T -ErrorAction SilentlyContinue
    if (-not $hadRelayLocal) { Remove-Item -Recurse -Force $relayLocal -ErrorAction SilentlyContinue }
    if (-not $hadRelayRoaming) { Remove-Item -Recurse -Force $relayRoaming -ErrorAction SilentlyContinue }

    Invoke-Check 'C3 the live mcremote task is unchanged' {
        if ($null -eq $mcremoteBefore) { Write-Host '   (no mcremote task registered; nothing to compare)'; return }
        $after = (schtasks /query /tn mcremote /xml ONE) -join "`n"
        if ($after -ne $mcremoteBefore) { throw 'the mcremote task definition changed during this run' }
    }

    Write-Host ''
    if ($script:Failures -eq 0) {
        Write-Host 'service acceptance: ALL CHECKS PASSED' -ForegroundColor Green
    } else {
        Write-Host "service acceptance: $($script:Failures) CHECK(S) FAILED" -ForegroundColor Red
    }
}
exit $script:Failures
