#Requires -Version 5.1
# End-to-end tests for scripts/install.ps1 (MADR 0156 D7).
#
# Builds a fake release tree, serves it from an HttpListener on
# http://127.0.0.1:<free port>/, and runs the real installer against it as a
# child process under the shell being tested. No request leaves loopback
# (PLAN C4), and install.ps1 is driven exactly as a user drives it: through
# -BaseUrl and the unmodified Get-File / Invoke-WebRequest (D6).
#
# Why loopback and not a directory: Windows PowerShell 5.1's Invoke-WebRequest
# reads a local path, but PowerShell 7 refuses with "The 'file' scheme is not
# supported" (MADR 0156 F13). Loopback behaves the same on both.
#
# The unit test (scripts/install_ps1_unit_test.ps1) pins the manifest decision.
# This test pins what that one cannot see: that the installer really wires the
# pinned version through, that downloads flow through the real fetch path, and
# that a failure part-way through installs nothing (PLAN C3).
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell
#   pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh
#
# -Installer runs the same cases against another copy of the script; it exists
# for the negative control against the pre-fix revision (PLAN 0156 P2).
[CmdletBinding()]
param(
    [ValidateSet('powershell', 'pwsh')]
    [string]$Shell = 'powershell',
    [string]$Installer,
    [int]$CaseTimeoutSeconds = 120
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $Installer) { $Installer = Join-Path $PSScriptRoot 'install.ps1' }
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$shellExe = (Get-Command $Shell -CommandType Application | Select-Object -First 1).Source

$script:Pass = 0
$script:Fail = 0
function ok  { param([string]$Name) $script:Pass++; Write-Host "  ok   $Name" }
function bad { param([string]$Name, [string]$Detail) $script:Fail++; Write-Host "  FAIL $Name"; if ($Detail) { Write-Host "       $Detail" } }
function check {
    param([string]$Name, $Got, $Want)
    if ([string]$Got -ceq [string]$Want) { ok $Name } else { bad $Name "want [$Want] got [$Got]" }
}
function contains {
    param([string]$Name, [string]$Haystack, [string]$Needle)
    if ($Haystack.Contains($Needle)) { ok $Name } else { bad $Name "missing [$Needle] in: $(Tail-Text $Haystack)" }
}
function Tail-Text {
    param([string]$Text)
    $lines = @($Text -split "`n" | ForEach-Object { $_.TrimEnd() } | Where-Object { $_ })
    ($lines | Select-Object -Last 6) -join ' | '
}

$work = Join-Path ([System.IO.Path]::GetTempPath()) ('mcremote-install-ps1-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work -Force | Out-Null

# ------------------------------------------------------------ fake binaries
#
# The installer runs `mcremote.exe version` after installing, so the fixture
# binaries must be real executables: a non-PE file would throw there and fail
# every success case for a reason unrelated to what is under test. HOSTNAME.EXE
# with a product-specific tail appended runs on both shells (it prints nothing
# to stdout and exits 1, so the installer's version comparison is skipped) and
# gives each product a distinct hash, so a swapped hash cannot pass unnoticed.

$stubSource = Join-Path $env:SystemRoot 'System32\HOSTNAME.EXE'
$stubs = @{}
foreach ($p in 'mcremote', 'mcrelay') {
    $path = Join-Path $work "stub-$p.exe"
    $bytes = [System.IO.File]::ReadAllBytes($stubSource) + [System.Text.Encoding]::ASCII.GetBytes("`nMC0156-STUB $p`n")
    [System.IO.File]::WriteAllBytes($path, $bytes)
    $stubs[$p] = [pscustomobject]@{ Path = $path; Hash = (Get-FileHash -Path $path -Algorithm SHA256).Hash.ToLower() }
}

# New-Release lays out <root>/<urlDir>/ with both binaries under their download
# (canonical) names and a SHA256SUMS built from $Lines.
function New-Release {
    param([string]$Root, [string]$UrlDir, [string[]]$Lines)
    $dir = Join-Path $Root ($UrlDir.Replace('/', '\'))
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    foreach ($p in 'mcremote', 'mcrelay') {
        Copy-Item -LiteralPath $stubs[$p].Path -Destination (Join-Path $dir "$p-windows-amd64.exe")
    }
    [System.IO.File]::WriteAllText((Join-Path $dir 'SHA256SUMS'), (($Lines -join "`n") + "`n"))
}

# ---------------------------------------------------------- loopback server

$script:Listener = $null
$script:ServerPs = $null
$script:ServerHandle = $null
$script:Requests = New-Object 'System.Collections.Concurrent.ConcurrentQueue[string]'
$script:Root = $null

function Start-FixtureServer {
    param([string]$Root)
    $script:Root = $Root
    while ($script:Requests.Count -gt 0) { $null = $script:Requests.TryDequeue([ref]$null) }

    # Free port: bind to 0, read it, release it.
    $probe = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
    $probe.Start(); $port = $probe.LocalEndpoint.Port; $probe.Stop()

    # Created and started HERE, in the test process, so this process can close
    # it. 127.0.0.1, never localhost or +: + needs elevation (MADR 0156).
    $script:Listener = New-Object System.Net.HttpListener
    $script:Listener.Prefixes.Add("http://127.0.0.1:$port/")
    $script:Listener.Start()

    $script:ServerPs = [powershell]::Create()
    [void]$script:ServerPs.AddScript({
        param($listener, $root, $requests)
        try {
            while ($listener.IsListening) {
                $ctx = $listener.GetContext()
                $rel = [Uri]::UnescapeDataString($ctx.Request.Url.AbsolutePath.TrimStart('/'))
                $requests.Enqueue($rel)
                $path = Join-Path $root ($rel.Replace('/', '\'))
                if ($rel -and (Test-Path -LiteralPath $path -PathType Leaf)) {
                    $bytes = [System.IO.File]::ReadAllBytes($path)
                    $ctx.Response.StatusCode = 200
                    $ctx.Response.ContentLength64 = $bytes.Length
                    $ctx.Response.OutputStream.Write($bytes, 0, $bytes.Length)
                } else {
                    $ctx.Response.StatusCode = 404
                }
                $ctx.Response.Close()
            }
        } catch [System.Net.HttpListenerException] {
        } catch [System.ObjectDisposedException] {
        } catch [System.InvalidOperationException] {
        }
    }).AddArgument($script:Listener).AddArgument($Root).AddArgument($script:Requests)
    $script:ServerHandle = $script:ServerPs.BeginInvoke()
    return "http://127.0.0.1:$port"
}

# PLAN C8: close from this process, then wait a bounded time. Never
# PowerShell.Stop() -- it does not interrupt a blocked GetContext() and was
# measured hanging in 4 of 4 shell combinations.
function Stop-FixtureServer {
    if ($script:Listener) {
        try { $script:Listener.Close() } catch { }
        $script:Listener = $null
    }
    if ($script:ServerPs) {
        if ($script:ServerHandle -and -not $script:ServerHandle.AsyncWaitHandle.WaitOne(5000)) {
            Write-Host '  WARN fixture server runspace did not exit within 5 s of Close()'
        }
        $script:ServerPs.Dispose()
        $script:ServerPs = $null
        $script:ServerHandle = $null
    }
}

# ---------------------------------------------------------- run the installer

# Invoke-Installer runs install.ps1 in a child $Shell process with a hard
# timeout. Start-Process with redirected streams, not `& $shell ... 2>&1`:
# under 5.1 that redirect turns every stderr line into an ErrorRecord, which
# ErrorActionPreference Stop in THIS script would throw on.
function Invoke-Installer {
    param([string]$BaseUrl, [string]$InstallDir, [string]$Version)
    $outFile = Join-Path $work ('out-' + [Guid]::NewGuid().ToString('N') + '.txt')
    $errFile = "$outFile.err"
    $argList = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', "`"$Installer`"",
        '-BaseUrl', "`"$BaseUrl`"", '-InstallDir', "`"$InstallDir`"")
    if ($Version) { $argList += @('-Version', $Version) }

    # Launching Windows PowerShell from a PowerShell 7 host: `& powershell`
    # hands the child a Windows PowerShell module path, but Start-Process
    # passes pwsh's PSModulePath through, and 5.1 then cannot autoload
    # Get-FileHash. Measured from 7.6.6: & -> OK; Start-Process inherited ->
    # "Get-FileHash is not recognized"; Start-Process with PSModulePath removed
    # -> OK. Removing it for the child reproduces what a user who types
    # `powershell` in pwsh actually gets; it is not a workaround in the
    # installer, which is untouched. The child's environment is copied at
    # start, so the variable is restored straight after.
    $savedModulePath = $null
    $clearModulePath = ($Shell -eq 'powershell') -and ($PSVersionTable.PSEdition -eq 'Core')
    if ($clearModulePath) {
        $savedModulePath = $env:PSModulePath
        Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue
    }
    try {
        $proc = Start-Process -FilePath $shellExe -ArgumentList $argList -NoNewWindow -PassThru `
            -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    } finally {
        if ($clearModulePath) { $env:PSModulePath = $savedModulePath }
    }
    # On .NET Framework (5.1) ExitCode reads back empty unless the process
    # handle was opened while the process was still running. Measured: all
    # three success cases reported exit [] with correct installs until this.
    $null = $proc.Handle
    $timedOut = -not $proc.WaitForExit($CaseTimeoutSeconds * 1000)
    if ($timedOut) {
        try { $proc.Kill() } catch { }
        $proc.WaitForExit(5000) | Out-Null
    }
    # WaitForExit(int) can return before redirected streams are flushed and
    # before ExitCode is populated; the parameterless overload waits for both.
    if (-not $timedOut) { $proc.WaitForExit() }
    $text = ''
    foreach ($f in $outFile, $errFile) {
        if (Test-Path -LiteralPath $f) { $text += [System.IO.File]::ReadAllText($f) + "`n" }
    }
    $code = if ($timedOut) { -1 } else { $proc.ExitCode }
    return [pscustomobject]@{ ExitCode = $code; TimedOut = $timedOut; Output = $text }
}

function Get-InstalledFiles {
    param([string]$InstallDir)
    if (-not (Test-Path -LiteralPath $InstallDir)) { return @() }
    return @(Get-ChildItem -LiteralPath $InstallDir -Recurse -Force -File)
}

# New-Case gives each case its own release root and an existing, empty install
# directory, so "installs nothing" is checked against a directory that is known
# to have been empty, not merely one that was never created.
function New-Case {
    param([string]$Name)
    $caseDir = Join-Path $work $Name
    $installDir = Join-Path $caseDir 'install'
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    return [pscustomobject]@{ Root = (Join-Path $caseDir 'release'); InstallDir = $installDir }
}

function Assert-Success {
    param([string]$Case, $Run, $InstallDir)
    check "$Case did not time out" $Run.TimedOut $false
    if ($Run.ExitCode -ne 0) {
        bad "$Case installer exits 0" "want [0] got [$($Run.ExitCode)]; output: $(Tail-Text $Run.Output)"
    } else {
        ok "$Case installer exits 0"
    }
    foreach ($p in 'mcremote', 'mcrelay') {
        $target = Join-Path (Join-Path $InstallDir $p) "$p.exe"
        if (Test-Path -LiteralPath $target) {
            check "$Case $p installed with the served bytes" (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLower() $stubs[$p].Hash
        } else {
            bad "$Case $p installed with the served bytes" "missing $target"
        }
    }
}

# PLAN C3: every refusal leaves the install directory exactly as it was --
# empty -- and that is asserted, not inferred from the non-zero exit.
function Assert-Refused {
    param([string]$Case, $Run, $InstallDir, [string]$Needle)
    check "$Case did not time out" $Run.TimedOut $false
    if ($Run.ExitCode -eq 0) { bad "$Case installer exits non-zero" 'got [0]' } else { ok "$Case installer exits non-zero" }
    contains "$Case reports the refusal" $Run.Output $Needle
    $left = @(Get-InstalledFiles $InstallDir)
    if ($left.Count -eq 0) {
        ok "$Case install directory untouched"
    } else {
        bad "$Case install directory untouched" ('found: ' + (($left | ForEach-Object { $_.FullName.Substring($InstallDir.Length) }) -join ', '))
    }
}

$HM = $stubs['mcremote'].Hash
$HR = $stubs['mcrelay'].Hash
$BAD = '0' * 64

Write-Host "install_ps1_test: host PowerShell $($PSVersionTable.PSVersion); installer under $Shell ($shellExe)"
Write-Host "install_ps1_test: installer $Installer"

try {
    # ------------------------------------------------------------------ 1
    Write-Host ''
    Write-Host '1. canonical manifest, latest'
    $c = New-Case 'case1'
    New-Release -Root $c.Root -UrlDir 'latest/download' -Lines @(
        "$HM  mcremote-windows-amd64.exe", "$HR  mcrelay-windows-amd64.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir } finally { Stop-FixtureServer }
    Assert-Success '1' $run $c.InstallDir
    contains '1 mcremote verified' $run.Output 'mcremote verified'
    contains '1 mcrelay verified'  $run.Output 'mcrelay verified'
    $seen = @($script:Requests.ToArray())
    foreach ($want in 'latest/download/SHA256SUMS', 'latest/download/mcremote-windows-amd64.exe', 'latest/download/mcrelay-windows-amd64.exe') {
        if ($seen -contains $want) { ok "1 fetched $want over loopback" } else { bad "1 fetched $want over loopback" "requests: $($seen -join ', ')" }
    }

    # ----------------------------------------------------------------- 1b
    # Pin wiring: a canonical entry reports only the pin, so the version in the
    # log proves main passes -Version through Resolve-Product to the selector.
    Write-Host ''
    Write-Host '1b. canonical manifest, pinned -Version'
    $c = New-Case 'case1b'
    New-Release -Root $c.Root -UrlDir 'download/v0.17.1' -Lines @(
        "$HM  mcremote-windows-amd64.exe", "$HR  mcrelay-windows-amd64.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir -Version '0.17.1' } finally { Stop-FixtureServer }
    Assert-Success '1b' $run $c.InstallDir
    contains '1b pinned version reported' $run.Output 'mcremote verified, version 0.17.1'

    # ------------------------------------------------------------------ 2
    Write-Host ''
    Write-Host '2. legacy versioned manifest, pinned -Version'
    $c = New-Case 'case2'
    New-Release -Root $c.Root -UrlDir 'download/v9.9.9.1' -Lines @(
        "$HM  mcremote-windows-amd64-9.9.9.1.exe", "$HR  mcrelay-windows-amd64-9.9.9.1.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir -Version '9.9.9.1' } finally { Stop-FixtureServer }
    Assert-Success '2' $run $c.InstallDir
    contains '2 resolved version reported' $run.Output 'mcremote verified, version 9.9.9.1'

    # ------------------------------------------------------------------ 3
    Write-Host ''
    Write-Host '3. both shapes for mcremote: refused'
    $c = New-Case 'case3'
    New-Release -Root $c.Root -UrlDir 'latest/download' -Lines @(
        "$HM  mcremote-windows-amd64.exe", "$HM  mcremote-windows-amd64-9.9.9.1.exe", "$HR  mcrelay-windows-amd64.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir } finally { Stop-FixtureServer }
    Assert-Refused '3' $run $c.InstallDir 'ambiguous SHA256SUMS'

    # ------------------------------------------------------------------ 4
    # The C3 case: mcremote verifies, mcrelay does not. Nothing may be
    # installed -- including the product that already verified.
    Write-Host ''
    Write-Host '4. mcrelay hash corrupted after mcremote verifies: refused, nothing installed'
    $c = New-Case 'case4'
    New-Release -Root $c.Root -UrlDir 'latest/download' -Lines @(
        "$HM  mcremote-windows-amd64.exe", "$BAD  mcrelay-windows-amd64.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir } finally { Stop-FixtureServer }
    Assert-Refused '4' $run $c.InstallDir 'checksum mismatch for mcrelay'
    contains '4 mcremote had verified first' $run.Output 'mcremote verified'

    # ------------------------------------------------------------------ 5
    Write-Host ''
    Write-Host '5. no entry for mcremote: refused'
    $c = New-Case 'case5'
    New-Release -Root $c.Root -UrlDir 'latest/download' -Lines @(
        "$HR  mcrelay-windows-amd64.exe")
    $url = Start-FixtureServer $c.Root
    try { $run = Invoke-Installer -BaseUrl $url -InstallDir $c.InstallDir } finally { Stop-FixtureServer }
    Assert-Refused '5' $run $c.InstallDir 'no checksum entry'
} finally {
    Stop-FixtureServer
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

# ------------------------------------------------------------------ summary

Write-Host ''
Write-Host "$script:Pass passed, $script:Fail failed"
if ($script:Fail -ne 0) { exit 1 }
exit 0
