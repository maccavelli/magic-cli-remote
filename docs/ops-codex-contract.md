# Re-pinning the Codex contract

The Codex provider carries a generated inventory of the app-server protocol —
every client request, server notification and server request the installed binary
declares, with what this project does about each one. It is embedded into the
daemon (`internal/provider/codex/contract.go`) and an invalid manifest **fails
engine start**, so it is load-bearing rather than advisory.

This page is how to move that pin to a new Codex release.

Decisions behind it: [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](spec/0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md),
and the earlier [0109](spec/0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md) that
introduced the manifest.

## The command

```powershell
./scripts/codex-contract.ps1 -Version 0.155.1 `
    -SourceCommit be2951ea3 `
    -SourceTree C:\Users\you\codex-worktree
```

Add `-DryRun` to print every resolved input and write nothing. Then:

1. Point the three `//go:embed` directives in `internal/provider/codex/contract.go`
   at the new `testdata/<version>/` directory.
2. Set `KnownGoodVersion` in `internal/provider/codex/version.go`.
3. `make live-codex-contract` — must pass.
4. `CODEX_CONTRACT_EXACT=1 make live-codex-contract` — must also pass, on the
   host that captured it. That is the proof the pin, the binary and the schema
   agree.
5. Delete the previous `testdata/<old version>/` directory. Leaving it invites
   the next reader to diff against the wrong baseline.

## Five things that will mislead you

Each of these cost real time; they are recorded so they cost it once.

**The schema bundles are committed blobs, not runtime introspection.**
`codex app-server generate-json-schema` decompresses
`codex-rs/app-server-protocol/schema/precomputed/app-server-exports-{stable,experimental}.json.zst`.
It does not inspect types at run time. A source tree at a different commit than
the installed binary therefore produces a **stale contract with no error**, which
is why the script refuses a `-SourceTree` whose `HEAD` disagrees with
`-SourceCommit`, and why the generator re-derives the binary's SHA-256 itself
instead of accepting a pasted value.

**Method literals live under `enum`, not `const`.** The extraction path is
`<Union>.definitions.<Union>.oneOf[].properties.method.enum[0]`. A `const`-based
extractor returns **zero methods** and looks like an empty release.

**`ServerRequest` is in the v1 aggregate, not the v2 one.** The generator reads
`codex_app_server_protocol.v2.schemas.json` for client requests and notifications,
then falls back to a standalone `ServerRequest.json` beside it. A v2-only reader
concludes, wrongly, that every server request was removed.

**Codex's exporter does not prune experimental *notifications*.**
`filter_experimental_schema` prunes experimental client methods, server methods
and fields — never notifications — so *both* bundles list all of them, while the
runtime does suppress the experimental ones. Notification stability therefore
comes from a scan of `#[experimental]` in
`codex-rs/app-server-protocol/src/protocol/common.rs`, which is why
`-SourceTree` is required and not optional. At 0.155.1 the capture reports
**60 stable / 22 experimental of the 82 in the bundle** — 62/22 of 84 on the wire,
because `export.rs` also excludes `rawResponseItem/completed` and
`rawResponse/completed` from the JSON entirely.

The captured files still record all 82 on both surfaces, because they mirror the
exporter: a manifest that disagrees with its own source cannot pass the gate's
exact mode. So the split is reported at capture time and written up in the
version's README, and persisting it as a manifest field waits for
`schema_version` 2. Until then, treat "stable notification" in the manifest as
"declared", not as "arrives without the opt-in".

**Three methods exist on the wire and in no schema.**
`export.rs` strips `getConversationSummary`, `gitDiffToRemote` and
`getAuthStatus` from every bundle as v1 legacy, yet the server still dispatches
them. `gitDiffToRemote` is one we call, so its absence from the inventory is
expected and **not** a bug to fix by deleting the call.

## What the generator derives rather than trusts

- **The binary SHA-256 and version**, from the codex on `PATH`. Set
  `CODEX_CONTRACT_BINARY_SHA256` only for a cross-host capture; a disagreement
  with the resolved binary is fatal. Note what gets hashed on Windows: `PATH`
  resolves an npm install to a ~341-byte `.cmd` shim, so the recorded hash
  identifies the shim rather than the 300 MB engine, and npm regenerates that
  shim identically across releases. The capture logs a NOTE when it sees this;
  version equality is the real evidence on such a host.
- **The `implemented` classification**, from this package's own code: the
  notification route table for notifications, a string literal passed to a call
  for client requests, and a `case` label for server requests. The hand-maintained
  switch it replaced named 32 client requests and no server request at all, so all
  21 server requests were recorded as deferred while nine were answered
  deliberately.

## Where the source-side surfaces come from

Both source surfaces are decompressed from the committed blobs
`codex-rs/app-server-protocol/schema/precomputed/app-server-exports-{stable,experimental}.json.zst`
— the same blobs `generate-json-schema` reads. Taking both from there is what
makes the source-watch `installed_delta` an observation rather than a formality.

That needs zstd, and this host has no `zstd` binary and no Python `zstandard`, so
the script uses Node's `zlib.zstdDecompressSync` (**Node >= 23.8**; measured on
v24.14.0). Without Node the capture fails rather than quietly substituting the
installed export.

The delta compares **method sets, not bytes**: the blob and the binary's own
export differ by four trailing bytes while declaring an identical 164 methods, so
a byte comparison would report drift on every capture.

**Filter both sides or the delta lies.** The exporter over-reports experimental
notifications identically on the installed and the source side, so filtering only
the installed surface reports all 22 experimental notifications as "in source,
missing from installed" — a phantom delta that never clears. A correct capture at
0.155.1 reports `installed_delta: 0`.

## Two installs, one PATH

A Windows host can carry two Codex installs at different versions — on the
reference host `%APPDATA%\npm` held 0.155.1 while
`%LOCALAPPDATA%\OpenAI\Codex\bin\…` held 0.154.0-alpha.6.2, the second driven by
the Codex desktop app. Only `PATH` order decides which one the daemon drives, so
the script prints the resolved path, warns when it finds a second install at a
different version, and refuses a version mismatch outright.

## The drift gate

`make live-codex-contract` runs in two modes:

| Mode | Fails on | Use |
| --- | --- | --- |
| default | **breaking** drift only — a removed or renamed method, a newly required param or result field, or a stable→experimental demotion. Additions are listed and pass. | CI, and any host |
| `CODEX_CONTRACT_EXACT=1` | any difference at all, plus a binary identity mismatch | release pinning, on the capture host |

The distinction is the point. The previous gate compared the whole surface with
`reflect.DeepEqual`, so it failed on any addition and could not pass on a host
running a Codex newer than the pin — and a gate that cannot go green is a gate
nobody runs. Seven declared notifications went unrouted for six releases behind
it.

Why each breaking class is breaking, and a relaxation is not:

- **Removed**: if we send or handle it, the call now fails at runtime; if we do
  not, our inventory describes a protocol that no longer exists. The
  classification travels with the message, because "removed something we
  implement" and "removed something we never used" need different responses.
- **Demoted to experimental-only**: the method still exists, so removal and
  addition checks both see nothing wrong — it simply needs an opt-in now, and
  fails with `-32600` at the call site rather than at start-up.
- **Newly required field**: we start sending a request the engine rejects, or
  reading a result field that may be absent.
- **A field that stops being required is additive.** We keep sending it. Codex
  0.155.1 did exactly this to `FunctionCallOutputResponseItem.call_id`.

Expect the default mode to report additive drift whenever the installed Codex is
ahead of the pin. Against a 0.149.1 pin on a 0.155.1 host it reports **35**
additions and passes.

## Four managed-policy fields are sent but described by no generated type

`configRequirements/read` returns `allowedApprovalsReviewers`, `hooks`, `network`
and `application`, but each carries
`#[experimental("configRequirements/read.<field>")]`
(`codex-rs/app-server-protocol/src/protocol/v2/config.rs:416,431,434,436`), so
**the JSON Schema and the TypeScript exports both omit them**. The request method
itself carries no `#[experimental]` attribute — only those four fields do — so the
server sends them regardless of what a client declared.

Anything generated from those exports therefore drops admin-managed hooks and
network policy silently, including the injected HTTP headers under `network`. We
hand-write these structs, so we are unaffected today; the consequences are that
the captured inventory *understates* this response, and that a future move to
codegen would regress it without any schema check noticing.

Verify the gate list has not changed:

```bash
grep -rn 'experimental("configRequirements/read' ~/gitrepos/codex/codex-rs
```

Background: MADR 0163 F35.
