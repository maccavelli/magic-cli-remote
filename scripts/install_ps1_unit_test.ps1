#Requires -Version 5.1
# Unit tests for the manifest decision in scripts/install.ps1 (MADR 0156 D11).
#
# Calls Select-ManifestEntry directly with in-memory SHA256SUMS lines: no
# files, no network, no install. The function is loaded from the REAL
# install.ps1 by parsing it and defining only the named functions from their
# AST extents. Dot-sourcing the script would run its install, and a copy of the
# function here would stay green while the shipped script drifts (PLAN C6), so
# neither is done. install.ps1 carries no test guard for this (PLAN C5).
#
# Runs identically under Windows PowerShell 5.1 and PowerShell 7:
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
#   pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
#
# The companion end-to-end test is scripts/install_ps1_test.ps1.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

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
    if ($Haystack.Contains($Needle)) { ok $Name } else { bad $Name "missing [$Needle] in: $Haystack" }
}
# throws runs a scriptblock that must throw, and returns the message ('' if it
# did not throw, which the caller's contains/check then reports).
function throws {
    param([scriptblock]$Body)
    try { & $Body | Out-Null; return '' } catch { return $_.Exception.Message }
}

# ------------------------------------------------ load functions from the AST

$installer = Join-Path $PSScriptRoot 'install.ps1'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($installer, [ref]$tokens, [ref]$errors)
if ($errors.Count -gt 0) {
    Write-Host "install_ps1_unit_test: $installer does not parse:"
    $errors | ForEach-Object { Write-Host "  line $($_.Extent.StartLineNumber): $($_.Message)" }
    exit 1
}
$defined = $ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $false)

# By name, and loudly: C7 means Select-ManifestEntry calls no helper, so loading
# every function would only hide a C7 violation (PLAN P1, MADR 0156 OQ3).
foreach ($want in @('Select-ManifestEntry')) {
    $fn = @($defined | Where-Object { $_.Name -ceq $want })
    if ($fn.Count -ne 1) {
        Write-Host "install_ps1_unit_test: expected exactly one function '$want' in $installer, found $($fn.Count)"
        exit 1
    }
    # Dot-sourced at script scope so the definition outlives this loop.
    . ([scriptblock]::Create($fn[0].Extent.Text))
}

Write-Host "install_ps1_unit_test: PowerShell $($PSVersionTable.PSVersion) against $installer"

# ------------------------------------------------------------------ fixtures

$H1 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
$H2 = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
$H3 = 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'

function sel {
    param([string[]]$Sums, [string]$Product = 'mcremote', [string]$Arch = 'amd64', [string]$Pin = '')
    Select-ManifestEntry -Sums $Sums -Product $Product -Arch $Arch -PinnedVersion $Pin
}

# pick is sel for cases that must NOT throw. An unexpected throw is reported
# as a named failure and the run continues. Without this, ErrorActionPreference
# Stop aborts the whole suite at the first such case, so a regression shows up
# as one crash instead of the list of cases it broke. The M1 mutation check in
# PLAN 0156 P1 found exactly that.
function pick {
    param([string]$Case, [string[]]$Sums, [string]$Product = 'mcremote', [string]$Arch = 'amd64', [string]$Pin = '')
    try {
        return (sel -Sums $Sums -Product $Product -Arch $Arch -Pin $Pin)
    } catch {
        bad "$Case threw unexpectedly" $_.Exception.Message
        return [pscustomobject]@{ Shape = '(threw)'; Name = '(threw)'; Hash = '(threw)'; Version = '(threw)' }
    }
}

# ---------------------------------------------------------------------- cases

Write-Host ''
Write-Host 'U1. canonical only, unpinned'
$e = pick 'U1' -Sums @("$H1  mcremote-windows-amd64.exe", "$H2  mcrelay-windows-amd64.exe")
check 'U1 shape'   $e.Shape   'canonical'
check 'U1 name'    $e.Name    'mcremote-windows-amd64.exe'
check 'U1 hash'    $e.Hash    $H1
check 'U1 version is empty, never invented' $e.Version ''

Write-Host ''
Write-Host 'U2. canonical only, pinned'
$e = pick 'U2' -Sums @("$H1  mcremote-windows-amd64.exe") -Pin '0.17.1'
check 'U2 shape'   $e.Shape   'canonical'
check 'U2 version is the pin' $e.Version '0.17.1'

Write-Host ''
Write-Host 'U3. legacy only'
$e = pick 'U3' -Sums @("$H2  mcremote-windows-amd64-0.14.10.1.exe")
check 'U3 shape'   $e.Shape   'legacy'
check 'U3 hash'    $e.Hash    $H2
check 'U3 version sliced from the name, .exe stripped' $e.Version '0.14.10.1'
$e = pick 'U3b' -Sums @("$H2  mcremote-windows-amd64-0.14.10.1.exe") -Pin '9.9.9'
check 'U3b legacy ignores the pin; the name is the record' $e.Version '0.14.10.1'

Write-Host ''
Write-Host 'U4. canonical AND legacy: ambiguous, fail closed'
$m = throws { sel @("$H1  mcremote-windows-amd64.exe", "$H2  mcremote-windows-amd64-0.14.10.1.exe") }
contains 'U4 throws ambiguous'       $m 'ambiguous SHA256SUMS'
contains 'U4 names the canonical'    $m 'mcremote-windows-amd64.exe'
contains 'U4 names the versioned'    $m 'mcremote-windows-amd64-0.14.10.1.exe'
contains 'U4 says nothing installed' $m 'Nothing was installed.'
$m = throws { sel @("$H2  mcremote-windows-amd64-0.14.10.1.exe", "$H1  mcremote-windows-amd64.exe") }
contains 'U4b order does not pick a winner' $m 'ambiguous SHA256SUMS'

Write-Host ''
Write-Host 'U5. neither shape present'
$m = throws { sel @("$H2  mcrelay-windows-amd64.exe", "$H3  mcremote-linux-amd64") }
contains 'U5 throws no checksum entry' $m 'no checksum entry for mcremote-windows-amd64.exe'
$m = throws { sel @() }
contains 'U5b empty manifest throws' $m 'no checksum entry'

Write-Host ''
Write-Host 'U6. no cross-product match'
$m = throws { sel @("$H2  mcrelay-windows-amd64.exe") -Product 'mcremote' }
contains 'U6 mcrelay line does not authorise mcremote' $m 'no checksum entry'

Write-Host ''
Write-Host 'U7. no cross-arch match'
$m = throws { sel @("$H3  mcremote-windows-arm64.exe") -Arch 'amd64' }
contains 'U7 arm64 line does not authorise amd64' $m 'no checksum entry'

Write-Host ''
Write-Host 'U8. anchored on the filename field, not a substring'
foreach ($name in @('mcremote-windows-amd64.exe.sig', 'xmcremote-windows-amd64.exe', 'dist/mcremote-windows-amd64.exe', 'MCREMOTE-WINDOWS-AMD64.EXE')) {
    $m = throws { sel @("$H1  $name") }
    contains "U8 [$name] is not canonical" $m 'no checksum entry'
}

Write-Host ''
Write-Host 'U9. legacy requires a digit after the hyphen'
$m = throws { sel @("$H1  mcremote-windows-amd64-beta.exe") }
contains 'U9 -beta is not a versioned entry' $m 'no checksum entry'

Write-Host ''
Write-Host 'U10. CRLF, blank lines, extra whitespace'
$e = pick 'U10' -Sums @('', "  $H1   mcremote-windows-amd64.exe`r", "`r", '   ')
check 'U10 shape'           $e.Shape 'canonical'
check 'U10 name has no CR'  $e.Name  'mcremote-windows-amd64.exe'
check 'U10 hash'            $e.Hash  $H1
$e = pick 'U10b' -Sums @("$($H1.ToUpper())  mcremote-windows-amd64.exe")
check 'U10b hash normalised to lower case' $e.Hash $H1

Write-Host ''
Write-Host 'U11. the real v0.17.1 manifest (MADR 0156)'
$real = @(
    'e123ee1484eb12d9f73b79a5630b71dea1764c31cab0bd10a6f17f981dcd3d44  mcremote-darwin-arm64'
    'b09561fc4731df8248787970acb40aeeeab4084f49551343b4d5c1a916b8dcf1  mcremote-linux-amd64'
    '19671150ead829b807d1f858f2b7848e04856a3c66f22b810d102b09bd7d5686  mcremote-linux-arm64'
    '8ee9588ea4ea47e4769f4c6b57965d18a3719dbc59db2bc665dda95bc29732bf  mcremote-windows-amd64.exe'
    '657f71816c1dc1f23ddb0742ed639c7986c5ffc34c95eb30c4225e97862aaac3  mcrelay-darwin-arm64'
    'b932274f251ed165997db5191954fb301501e2a5616e0a498fc2992dab198d2a  mcrelay-linux-amd64'
    '9a5752ddcbb0cc228e23b96f5137c72456b9f68c15cf03f11628961faebcdfff  mcrelay-linux-arm64'
    '49f63af6bf85cfb48cc690c235088df06e692a5954a632d03ad4b0fb706bf323  mcrelay-windows-amd64.exe'
)
$e = pick 'U11' -Sums $real -Product 'mcremote'
check 'U11 mcremote canonical' $e.Shape 'canonical'
check 'U11 mcremote hash'      $e.Hash  '8ee9588ea4ea47e4769f4c6b57965d18a3719dbc59db2bc665dda95bc29732bf'
$e = pick 'U11' -Sums $real -Product 'mcrelay'
check 'U11 mcrelay canonical'  $e.Shape 'canonical'
check 'U11 mcrelay hash'       $e.Hash  '49f63af6bf85cfb48cc690c235088df06e692a5954a632d03ad4b0fb706bf323'

Write-Host ''
Write-Host 'U12. nothing that breaks or goes silently empty under irm | iex (MADR 0156 D15)'
# Under iex, $PSScriptRoot, $PSCommandPath and $MyInvocation's path are empty
# rather than an error, and $PSCmdlet does not exist at all -- v0.17.2 died on
# it. Only a static check sees the silent ones before a user does.
function Find-IexHostile {
    param([string]$Text)
    $tk = $null
    $pe = $null
    $root = [System.Management.Automation.Language.Parser]::ParseInput($Text, [ref]$tk, [ref]$pe)
    $found = @()
    $vars = $root.FindAll({ param($n) $n -is [System.Management.Automation.Language.VariableExpressionAst] }, $true)
    foreach ($v in $vars) {
        $name = $v.VariablePath.UserPath
        if (@('PSScriptRoot', 'PSCommandPath', 'MyInvocation') -contains $name) {
            $found += "$name (line $($v.Extent.StartLineNumber))"
            continue
        }
        if ($name -eq 'PSCmdlet') {
            # Allowed only inside an expression that first asks whether it exists.
            $guarded = $false
            $p = $v.Parent
            while ($p -and -not ($p -is [System.Management.Automation.Language.StatementBlockAst])) {
                if ($p.Extent.Text -match 'Test-Path\s+variable:PSCmdlet') { $guarded = $true; break }
                $p = $p.Parent
            }
            if (-not $guarded) { $found += "unguarded PSCmdlet (line $($v.Extent.StartLineNumber))" }
        }
    }
    return , $found
}
$hostile = Find-IexHostile ([System.IO.File]::ReadAllText($installer))
check 'U12 install.ps1 has no iex-hostile construct' ($hostile -join '; ') ''
$probe = Find-IexHostile 'Join-Path $PSScriptRoot "x"'
check 'U12b the check catches $PSScriptRoot' $probe.Count 1
$probe = Find-IexHostile 'if ($PSCmdlet.ShouldProcess("t", "install")) { 1 }'
check 'U12c the check catches an unguarded $PSCmdlet' $probe.Count 1
$probe = Find-IexHostile 'if (-not (Test-Path variable:PSCmdlet) -or $PSCmdlet.ShouldProcess("t", "install")) { 1 }'
check 'U12d a guarded $PSCmdlet is allowed' $probe.Count 0

Write-Host ''
Write-Host 'U13. the PATH notice names every product folder (MADR 0159 D15, F20)'
# Loaded separately from Select-ManifestEntry, so the C7 check above still
# loads that function alone. Write-Warn is Add-ToPathNotice's only helper.
foreach ($want in @('Write-Warn', 'Add-ToPathNotice')) {
    $fn = @($defined | Where-Object { $_.Name -ceq $want })
    if ($fn.Count -ne 1) {
        bad "U13 load $want" "expected exactly one function '$want', found $($fn.Count)"
        continue
    }
    . ([scriptblock]::Create($fn[0].Extent.Text))
}
$absent = 'C:\mcremote-unit-test-' + [Guid]::NewGuid().ToString('N')
$notice = (Add-ToPathNotice -Dir $absent 3>&1 6>&1 | Out-String -Width 4096)
contains 'U13 the notice names the folder' $notice $absent
contains 'U13b the advice appends to the User Path' $notice "[Environment]::GetEnvironmentVariable('Path', 'User') + ';$absent'"
if ($notice.Contains('$env:Path')) { bad 'U13c the advice does not use $env:Path' $notice } else { ok 'U13c the advice does not use $env:Path' }

# The only call must be inside a foreach over $Products, so a product added to
# the list is named too.
$calls = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] -and $n.GetCommandName() -eq 'Add-ToPathNotice' }, $true))
check 'U13d exactly one Add-ToPathNotice call' $calls.Count 1
if ($calls.Count -eq 1) {
    $p = $calls[0].Parent
    while ($p -and -not ($p -is [System.Management.Automation.Language.ForEachStatementAst])) { $p = $p.Parent }
    $over = ''
    if ($p) { $over = $p.Condition.Extent.Text }
    check 'U13e the call iterates $Products' $over '$Products'
}

Write-Host ''
Write-Host 'U14. the installer puts its folders on the User Path (MADR 0159 D16)'
foreach ($want in @('Test-PathEntry', 'Add-ToUserPath')) {
    $fn = @($defined | Where-Object { $_.Name -ceq $want })
    if ($fn.Count -ne 1) {
        bad "U14 load $want" "expected exactly one function '$want', found $($fn.Count)"
        continue
    }
    . ([scriptblock]::Create($fn[0].Extent.Text))
}
# A scratch key stands in for HKCU:\Environment, so the tester's real Path is
# never written. The running process's $env:Path is restored at the end.
$scratch = 'HKCU:\Software\mcremote-install-unit-' + [Guid]::NewGuid().ToString('N')
$savedEnvPath = $env:Path
$dir = 'C:\mcremote-unit-path-' + [Guid]::NewGuid().ToString('N')
function Get-ScratchPath {
    $k = Get-Item -LiteralPath $scratch
    $v = $k.GetValue('Path', $null, 'DoNotExpandEnvironmentNames')
    $kind = ''
    if ($null -ne $v) { $kind = [string]$k.GetValueKind('Path') }
    return @{ Value = $v; Kind = $kind }
}
try {
    New-Item -Path $scratch -Force | Out-Null

    # A profile's default shape: REG_EXPAND_SZ with a %VAR% entry.
    New-ItemProperty -LiteralPath $scratch -Name Path -PropertyType ExpandString -Value '%USERPROFILE%\x;C:\y' | Out-Null
    check 'U14 absent: changed' (Add-ToUserPath -Dir $dir -Key $scratch) $true
    $after = Get-ScratchPath
    check 'U14 absent: appended, %VAR% entry kept literally' $after.Value "%USERPROFILE%\x;C:\y;$dir"
    check 'U14 absent: the value kind is kept' $after.Kind 'ExpandString'
    contains 'U14 absent: the running session sees it' $env:Path $dir

    check 'U14b a second run changes nothing' (Add-ToUserPath -Dir $dir -Key $scratch) $false
    check 'U14b the value is unchanged' (Get-ScratchPath).Value "%USERPROFILE%\x;C:\y;$dir"

    check 'U14c a trailing \ counts as present' (Add-ToUserPath -Dir ($dir + '\') -Key $scratch) $false
    check 'U14c different case counts as present' (Add-ToUserPath -Dir $dir.ToUpperInvariant() -Key $scratch) $false
    check 'U14c an expanded %VAR% entry counts as present' (Add-ToUserPath -Dir ([Environment]::ExpandEnvironmentVariables('%USERPROFILE%\x')) -Key $scratch) $false

    # A REG_SZ value stays REG_SZ.
    Set-ItemProperty -LiteralPath $scratch -Name Path -Value 'C:\y;' -Type String
    check 'U14d REG_SZ: changed' (Add-ToUserPath -Dir $dir -Key $scratch) $true
    $after = Get-ScratchPath
    check 'U14d REG_SZ: appended once, no empty entry' $after.Value "C:\y;$dir"
    check 'U14d REG_SZ: the kind is kept' $after.Kind 'String'

    # No Path value at all: created as REG_EXPAND_SZ, the Windows default.
    Remove-ItemProperty -LiteralPath $scratch -Name Path
    check 'U14e missing: changed' (Add-ToUserPath -Dir $dir -Key $scratch) $true
    $after = Get-ScratchPath
    check 'U14e missing: created with the folder' $after.Value $dir
    check 'U14e missing: created as ExpandString' $after.Kind 'ExpandString'
} finally {
    Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue
    $env:Path = $savedEnvPath
}
check 'U14 the scratch key is gone' (Test-Path -LiteralPath $scratch) $false

# The main block calls Add-ToUserPath once, inside the foreach over $Products,
# and only on the branch where the opt-out is off.
$calls = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] -and $n.GetCommandName() -eq 'Add-ToUserPath' }, $true))
check 'U14f exactly one Add-ToUserPath call' $calls.Count 1
if ($calls.Count -eq 1) {
    $p = $calls[0].Parent
    $guard = ''
    while ($p -and -not ($p -is [System.Management.Automation.Language.ForEachStatementAst])) {
        if ($p -is [System.Management.Automation.Language.IfStatementAst]) { $guard = $p.Clauses[0].Item1.Extent.Text }
        $p = $p.Parent
    }
    $over = ''
    if ($p) { $over = $p.Condition.Extent.Text }
    check 'U14f the call iterates $Products' $over '$Products'
    check 'U14f the first branch is the opt-out' $guard '$pathOptOut'
}
$optOut = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.AssignmentStatementAst] -and $n.Left.Extent.Text -eq '$pathOptOut' }, $true))
check 'U14g the opt-out reads the switch and the variable' ($optOut.Count -eq 1 -and $optOut[0].Right.Extent.Text.Contains('$NoPathUpdate') -and $optOut[0].Right.Extent.Text.Contains("`$env:MCREMOTE_INSTALL_NO_PATH_UPDATE -eq '1'")) $true

# ------------------------------------------------------------------ summary

Write-Host ''
Write-Host "$script:Pass passed, $script:Fail failed"
if ($script:Fail -ne 0) { exit 1 }
exit 0
