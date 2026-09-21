<#
.SYNOPSIS
  Capture the Codex app-server contract for a release, and re-pin the fixtures.

.DESCRIPTION
  One command for what used to be four environment variables plus four hardcoded
  literals inside a Go test body (MADR 0163 D2/F2). It resolves the codex binary
  the daemon would resolve, exports its JSON Schema bundles, and runs the
  generator with every input set.

  Two rules this script exists to enforce, both learned the hard way:

  * The schema bundles are COMMITTED BLOBS, not runtime introspection
    (app-server-protocol/schema/precomputed/*.json.zst). A source tree at a
    different commit than the installed binary therefore yields a stale contract
    silently. -SourceTree must be a checkout at -SourceCommit.
  * Two codex installs can coexist at different versions. On the reference host
    %APPDATA%\npm held 0.155.1 while %LOCALAPPDATA%\OpenAI\Codex held
    0.154.0-alpha.6.2, and only PATH order decided which one the daemon drove.
    This script prints the resolved path and refuses a version mismatch.

.PARAMETER Version
  The codex version being pinned, e.g. 0.155.1. Must match `codex --version`.

.PARAMETER SourceCommit
  The upstream commit the source tree is checked out at. Recorded in the
  source-watch manifest; a mismatch with -SourceTree is fatal.

.PARAMETER SourceTree
  A codex checkout at -SourceCommit. Used for the #[experimental] notification
  scan, which the schema bundles cannot answer (MADR 0163 D4/F4).

.PARAMETER OutDir
  Where the three fixtures are written. Defaults to
  internal/provider/codex/testdata/<Version>.

.PARAMETER DryRun
  Print every resolved input and write nothing.

.EXAMPLE
  ./scripts/codex-contract.ps1 -Version 0.155.1 `
      -SourceCommit be2951ea3 -SourceTree C:\Users\me\cx155
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][string]$Version,
  [Parameter(Mandatory = $true)][string]$SourceCommit,
  [Parameter(Mandatory = $true)][string]$SourceTree,
  [string]$OutDir,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

function Fail([string]$message) { Write-Error $message; exit 1 }
function Note([string]$message) { Write-Host "codex-contract: $message" }

# --- 1. Resolve the binary the daemon would resolve -------------------------
$resolved = Get-Command codex -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $resolved) { Fail 'codex is not on PATH; the capture must run against the binary being pinned.' }
$reported = (& codex --version 2>&1 | Select-Object -First 1)
Note "PATH codex: $($resolved.Source)"
Note "reports:    $reported"
if ($reported -notmatch [regex]::Escape($Version)) {
  Fail "codex reports '$reported' but -Version is '$Version'. Capture against the binary being pinned."
}

# A second install is not an error, but silence about it would be.
$managed = Join-Path $env:LOCALAPPDATA 'OpenAI\Codex\bin'
if (Test-Path $managed) {
  Get-ChildItem $managed -Filter codex.exe -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
    $other = (& $_.FullName --version 2>&1 | Select-Object -First 1)
    if ($other -notmatch [regex]::Escape($Version)) {
      Note "NOTE a second install exists at a different version: $($_.FullName) -> $other"
      Note '     PATH order decides which one the daemon drives.'
    }
  }
}

# --- 2. Validate the source tree -------------------------------------------
if (-not (Test-Path (Join-Path $SourceTree 'codex-rs'))) {
  Fail "-SourceTree '$SourceTree' does not look like a codex checkout (no codex-rs/)."
}
$treeCommit = (& git -C $SourceTree rev-parse HEAD 2>$null)
if ($LASTEXITCODE -eq 0 -and $treeCommit -and -not $treeCommit.StartsWith($SourceCommit)) {
  Fail "-SourceTree is at $treeCommit but -SourceCommit is $SourceCommit. The schema blobs are committed, so a mismatched tree yields a stale contract silently."
}

if (-not $OutDir) { $OutDir = Join-Path $repo "internal/provider/codex/testdata/$Version" }

# --- 3. Export the installed binary's schema bundles ------------------------
$work = Join-Path ([System.IO.Path]::GetTempPath()) "codex-contract-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$installedStableDir = Join-Path $work 'installed-stable'
$installedExpDir    = Join-Path $work 'installed-experimental'

function Composite([string]$dir) {
  $path = Join-Path $dir 'codex_app_server_protocol.v2.schemas.json'
  if (-not (Test-Path $path)) { Fail "expected $path from generate-json-schema" }
  # The generator also needs ServerRequest.json beside it: ServerRequest lives in
  # the v1 aggregate, not the v2 one, so it is read as a standalone file.
  if (-not (Test-Path (Join-Path $dir 'ServerRequest.json'))) {
    Fail "ServerRequest.json is missing from $dir; the generator reads it beside the composite schema."
  }
  return $path
}

$env:CODEX_CONTRACT_VERSION        = $Version
$env:CODEX_SOURCE_COMMIT           = $SourceCommit
$env:CODEX_SOURCE_TREE             = $SourceTree
$env:CODEX_CONTRACT_OUT_DIR        = $OutDir

if ($DryRun) {
  Note 'DRY RUN - nothing will be written'
  Note "  version       $Version"
  Note "  binary        $($resolved.Source)"
  Note "  source tree   $SourceTree @ $SourceCommit"
  Note "  out dir       $OutDir"
  Note '  schemas       would be exported to a temp dir via `codex app-server generate-json-schema`'
  Note '  binary SHA    derived by the generator from the resolved binary'
  exit 0
}

Note "exporting schemas to $work"
& codex app-server generate-json-schema --out $installedStableDir | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'generate-json-schema (stable) failed' }
& codex app-server generate-json-schema --experimental --out $installedExpDir | Out-Null
if ($LASTEXITCODE -ne 0) { Fail 'generate-json-schema (experimental) failed' }

$env:CODEX_CONTRACT_STABLE_SCHEMA       = Composite $installedStableDir
$env:CODEX_CONTRACT_EXPERIMENTAL_SCHEMA = Composite $installedExpDir

# --- 4. Source-side surfaces, from the committed blobs --------------------
# Both source surfaces come from
# codex-rs/app-server-protocol/schema/precomputed/app-server-exports-{stable,
# experimental}.json.zst, which are the blobs `generate-json-schema` itself
# decompresses. Taking BOTH from there — rather than the unpacked schema/json/
# for stable and nothing for experimental — is what makes the source-watch
# installed_delta a real observation instead of empty by construction.
#
# zstd is the only awkward part: there is no zstd binary and no Python zstandard
# on this host, so decompression uses Node's zlib.zstdDecompressSync, which needs
# Node >= 23.8 (measured working on v24.14.0). The delta compares METHOD SETS, not
# bytes, which matters because the blob and the binary's own export differ by four
# trailing bytes while declaring an identical 164 methods.
function Expand-SourceExports([string]$tree, [string]$which, [string]$dest) {
  $blob = Join-Path $tree "codex-rs/app-server-protocol/schema/precomputed/app-server-exports-$which.json.zst"
  if (-not (Test-Path $blob)) { Fail "missing $blob; -SourceTree must be a codex checkout" }
  $node = Get-Command node -ErrorAction SilentlyContinue
  if (-not $node) { Fail 'node is required to decompress the source schema blobs (Node >= 23.8 for zlib.zstdDecompressSync)' }

  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  $js = Join-Path $dest '_expand.js'
  @'
const z = require('zlib'), fs = require('fs'), path = require('path');
if (typeof z.zstdDecompressSync !== 'function') {
  console.error('this node lacks zlib.zstdDecompressSync; Node >= 23.8 is required');
  process.exit(2);
}
const [blob, dest] = process.argv.slice(2);
const exports_ = JSON.parse(z.zstdDecompressSync(fs.readFileSync(blob)));
const schemas = exports_.json_schema || {};
const wanted = ['codex_app_server_protocol.v2.schemas.json', 'ServerRequest.json'];
for (const name of wanted) {
  if (!schemas[name]) { console.error('blob has no ' + name); process.exit(3); }
  fs.writeFileSync(path.join(dest, name), schemas[name]);
}
console.log(Object.keys(schemas).length);
'@ | Set-Content -Path $js -Encoding UTF8

  $count = & $node.Source $js $blob $dest
  if ($LASTEXITCODE -ne 0) { Fail "decompressing $blob failed" }
  Remove-Item $js -Force -ErrorAction SilentlyContinue
  Note "source $which : expanded $count schema files from the committed blob"
  return (Join-Path $dest 'codex_app_server_protocol.v2.schemas.json')
}

$env:CODEX_SOURCE_STABLE_SCHEMA       = Expand-SourceExports $SourceTree 'stable'       (Join-Path $work 'source-stable')
$env:CODEX_SOURCE_EXPERIMENTAL_SCHEMA = Expand-SourceExports $SourceTree 'experimental' (Join-Path $work 'source-experimental')

# --- 5. Generate -----------------------------------------------------------
$env:CODEX_CONTRACT_GENERATE = '1'
Push-Location (Join-Path $repo 'internal/provider/codex')
try {
  & go test -run TestGenerateContractManifest -v -count=1 .
  if ($LASTEXITCODE -ne 0) { Fail 'the generator failed; nothing was pinned' }
} finally {
  Pop-Location
  Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}

Note "wrote fixtures to $OutDir"
Note 'next: point the //go:embed directives in contract.go at the new directory,'
Note '      set KnownGoodVersion in version.go, then run: make live-codex-contract'
