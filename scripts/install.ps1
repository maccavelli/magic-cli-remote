#Requires -Version 5.1
<#
.SYNOPSIS
    Install mcremote and mcrelay on Windows.

.DESCRIPTION
    Downloads the published binaries for this machine's architecture, verifies
    them against the release SHA256SUMS manifest, and installs them under
    %LOCALAPPDATA%\Programs (MADR 0116 D13) — per-user, no elevation.

    Verification is by hash VALUE, never by filename. SHA256SUMS lists
    versioned names (mcremote-windows-amd64-0.14.10.1.exe) while this script
    downloads the unversioned alias, so matching the manifest line by name
    would fail; matching by value also yields the resolved version with no API
    call. This mirrors scripts/install.sh exactly.

.PARAMETER Version
    Install a specific release (e.g. 0.14.10) instead of the latest.

.PARAMETER InstallDir
    Override the install directory.

.PARAMETER WhatIf
    Show what would happen without downloading or installing anything.
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$Version,
    [string]$InstallDir,
    [string]$BaseUrl = 'https://github.com/maccavelli/magic-cli-remote/releases'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Products = @('mcremote', 'mcrelay')

function Write-Log { param([string]$Message) Write-Host "install: $Message" }
function Write-Warn { param([string]$Message) Write-Warning "install: $Message" }

function Get-TargetArch {
    # PROCESSOR_ARCHITECTURE reports the *process* architecture; on an ARM64
    # host running x64 PowerShell it says AMD64, which is still the binary that
    # will run. PROCESSOR_ARCHITEW6432 exposes the real machine when the
    # process is WOW64.
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' {
            throw @'
windows/arm64 is not a published target (MADR 0116 D19).
An amd64 build will run under emulation on Windows on Arm, but this installer
does not select it silently. Download mcremote-windows-amd64.exe by hand if
that is what you want.
'@
        }
        default { throw "unsupported processor architecture '$arch'; only amd64 is published." }
    }
}

function Get-UrlDir {
    if ($Version) { return "$BaseUrl/download/v$Version" }
    return "$BaseUrl/latest/download"
}

function Get-File {
    param([string]$Url, [string]$Destination)
    Write-Verbose "fetching $Url"
    $previous = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'   # a progress bar makes this ~10x slower
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing
    } finally {
        $ProgressPreference = $previous
    }
}

# Resolve-Product verifies a downloaded binary against the manifest by hash
# value and returns the version recorded for it.
function Resolve-Product {
    param(
        [string]$Product,
        [string]$Arch,
        [string]$BinaryPath,
        [string[]]$Sums
    )
    $prefix = "$Product-windows-$Arch-"
    $line = $Sums | Where-Object { $_ -match [regex]::Escape($prefix) } | Select-Object -First 1
    if (-not $line) {
        throw "no checksum entry for $prefix* in SHA256SUMS"
    }
    $fields = $line -split '\s+' | Where-Object { $_ }
    $want = $fields[0]
    $name = $fields[-1]

    $got = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLower()
    if ($want.ToLower() -ne $got) {
        throw @"
checksum mismatch for $Product
  expected $($want.ToLower())
  got      $got
Nothing was installed.
"@
    }
    # Convention C5 (MADR 0116 F17): the extension comes LAST, so strip it to
    # get the version. Without this the resolved version reads "0.14.10.1.exe".
    $resolved = $name.Substring($prefix.Length)
    if ($resolved.EndsWith('.exe')) {
        $resolved = $resolved.Substring(0, $resolved.Length - 4)
    }
    return $resolved
}

function Add-ToPathNotice {
    param([string]$Dir)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -and ($userPath -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') })) {
        return
    }
    Write-Warn "$Dir is not on your PATH. To add it for this user:"
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$Dir`", 'User')"
}

# ------------------------------------------------------------------- main

$arch = Get-TargetArch
$urlDir = Get-UrlDir
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs'
}
Write-Log "source $urlDir"
Write-Log "target windows/$arch -> $InstallDir"

if ($WhatIfPreference) {
    foreach ($p in $Products) {
        Write-Log "would download $urlDir/$p-windows-$arch.exe"
        Write-Log "would install  $(Join-Path (Join-Path $InstallDir $p) "$p.exe")"
    }
    Write-Log 'nothing was downloaded (-WhatIf)'
    return
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("mcremote-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $sumsPath = Join-Path $tmp 'SHA256SUMS'
    try {
        Get-File -Url "$urlDir/SHA256SUMS" -Destination $sumsPath
    } catch {
        throw @"
could not download SHA256SUMS from $urlDir
If you pinned a version, check that the release exists and carries the
unversioned alias assets (releases before MADR 0116 do not, for Windows).
"@
    }
    $sums = Get-Content -Path $sumsPath

    $resolvedVersion = $null
    foreach ($p in $Products) {
        $dl = Join-Path $tmp "$p.exe"
        Get-File -Url "$urlDir/$p-windows-$arch.exe" -Destination $dl
        $resolvedVersion = Resolve-Product -Product $p -Arch $arch -BinaryPath $dl -Sums $sums
        Write-Log "$p verified, version $resolvedVersion"
    }

    foreach ($p in $Products) {
        $productDir = Join-Path $InstallDir $p
        New-Item -ItemType Directory -Path $productDir -Force | Out-Null
        $target = Join-Path $productDir "$p.exe"
        if ($PSCmdlet.ShouldProcess($target, 'install')) {
            # Move-Item -Force replaces a running binary's directory entry the
            # same way the self-updater does: a running .exe cannot be deleted
            # or written on Windows, but it can be renamed out of the way.
            if (Test-Path $target) {
                $backup = "$target.prev"
                Remove-Item -Path $backup -Force -ErrorAction SilentlyContinue
                Move-Item -Path $target -Destination $backup -Force
            }
            Move-Item -Path (Join-Path $tmp "$p.exe") -Destination $target -Force
            Write-Log "installed $target"
        }
    }

    $mcremote = Join-Path (Join-Path $InstallDir 'mcremote') 'mcremote.exe'
    if (Test-Path $mcremote) {
        $reported = (& $mcremote version) -split '\s+' | Select-Object -Skip 1 -First 1
        if ($resolvedVersion -and $reported -and ($reported -ne $resolvedVersion)) {
            Write-Warn "installed binary reports '$reported' but the manifest said '$resolvedVersion'"
        }
    }

    Add-ToPathNotice -Dir (Join-Path $InstallDir 'mcremote')

    Write-Host ''
    Write-Log 'Next: mcremote setup-service   (no elevation required)'
    Write-Host ''
    Write-Warn @'
These binaries are not Authenticode-signed yet (MADR 0116 D14), so Windows
SmartScreen may warn on first run and Smart App Control may block them.
'@
} finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
