#Requires -Version 5.1
<#
.SYNOPSIS
    Install mcremote and mcrelay on Windows.

.DESCRIPTION
    Downloads the published binaries for this machine's architecture, verifies
    them against the release SHA256SUMS manifest, and installs them under
    %LOCALAPPDATA%\Programs (MADR 0116 D13) — per-user, no elevation.

    Verification is by hash VALUE, compared against the manifest line for the
    product. Two manifest shapes exist and both verify (MADR 0005, MADR 0156):

      canonical (v0.16.0 onward)  SHA256SUMS lists the downloaded basename,
                                  mcremote-windows-amd64.exe. Preferred.
      legacy    (pre-v0.16.0)     SHA256SUMS lists versioned names,
                                  mcremote-windows-amd64-0.14.10.1.exe, while
                                  the unversioned alias is what gets downloaded.

    A manifest carrying BOTH shapes for one product is refused and nothing is
    installed: an appended canonical line could otherwise shadow the real
    versioned entry and authorise a substituted binary.

    Mirrored from scripts/install.sh: the two shapes and the preference for
    canonical, the fail-closed ambiguity rule, case-sensitive name matching,
    and the version rule (a canonical entry yields the pinned -Version or
    nothing, never an invented one; the trailing .exe is stripped).

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

# Select-ManifestEntry decides which SHA256SUMS line authorises a product, and
# nothing else. It is pure (MADR 0156 D10, PLAN C7): every input is a
# parameter, it reads no script-scope variable, and it does no I/O, so
# scripts/install_ps1_unit_test.ps1 can call it with in-memory lines.
#
# Two manifest shapes exist (see .DESCRIPTION). Each is looked up on its own,
# against the filename field rather than a substring of the whole line, and
# both being present is fatal rather than a tie to break. Names match
# case-sensitively, as install.sh's grep does (PLAN C2).
function Select-ManifestEntry {
    param(
        [string[]]$Sums,
        [string]$Product,
        [string]$Arch,
        [string]$PinnedVersion
    )
    $canonicalName = "$Product-windows-$Arch.exe"
    $legacyPrefix = "$Product-windows-$Arch-"
    $legacyPattern = '^' + [regex]::Escape($legacyPrefix) + '[0-9]'

    $canonical = $null
    $legacy = $null
    foreach ($raw in $Sums) {
        if ($null -eq $raw) { continue }
        $line = $raw.Trim()
        if (-not $line) { continue }
        $fields = @($line -split '\s+' | Where-Object { $_ })
        if ($fields.Count -lt 2) { continue }
        $entry = [pscustomobject]@{ Hash = $fields[0].ToLower(); Name = $fields[-1] }
        if (($null -eq $canonical) -and ($entry.Name -ceq $canonicalName)) {
            $canonical = $entry
        } elseif (($null -eq $legacy) -and ($entry.Name -cmatch $legacyPattern)) {
            $legacy = $entry
        }
    }

    if ($canonical -and $legacy) {
        throw @"
ambiguous SHA256SUMS: both a canonical and a versioned entry exist for $Product-windows-$Arch
  canonical  $($canonical.Name)
  versioned  $($legacy.Name)
A conforming release lists one shape per manifest. Refusing to choose.
Nothing was installed.
"@
    }

    if ($canonical) {
        # A canonical name carries no version. Report the pinned one if the
        # caller asked for a release, and otherwise nothing: never invent one.
        $resolved = ''
        if ($PinnedVersion) { $resolved = $PinnedVersion }
        $selected = $canonical
        $shape = 'canonical'
    } elseif ($legacy) {
        $resolved = $legacy.Name.Substring($legacyPrefix.Length)
        $selected = $legacy
        $shape = 'legacy'
    } else {
        throw "no checksum entry for $canonicalName (or a versioned $legacyPrefix<version>.exe) in SHA256SUMS"
    }

    # Convention C5 (MADR 0116 F17): the extension comes LAST, so strip it to
    # get the version. Without this the resolved version reads "0.14.10.1.exe".
    if ($resolved.EndsWith('.exe')) {
        $resolved = $resolved.Substring(0, $resolved.Length - 4)
    }

    return [pscustomobject]@{
        Hash    = $selected.Hash
        Name    = $selected.Name
        Shape   = $shape
        Version = $resolved
    }
}

# Resolve-Product verifies a downloaded binary against the manifest by hash
# value and returns the version recorded for it. The choice of manifest line
# is Select-ManifestEntry's; this function owns only the file hash.
function Resolve-Product {
    param(
        [string]$Product,
        [string]$Arch,
        [string]$BinaryPath,
        [string[]]$Sums,
        [string]$PinnedVersion
    )
    $entry = Select-ManifestEntry -Sums $Sums -Product $Product -Arch $Arch -PinnedVersion $PinnedVersion

    $got = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLower()
    if ($entry.Hash -ne $got) {
        throw @"
checksum mismatch for $Product
  expected $($entry.Hash)
  got      $got
Nothing was installed.
"@
    }
    return $entry.Version
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
        $resolvedVersion = Resolve-Product -Product $p -Arch $arch -BinaryPath $dl -Sums $sums -PinnedVersion $Version
        if ($resolvedVersion) {
            Write-Log "$p verified, version $resolvedVersion"
        } else {
            Write-Log "$p verified"
        }
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
