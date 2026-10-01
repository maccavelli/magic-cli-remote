---
status: accepted
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# The phone verifies the downloaded APK by GitHub's per-asset digest, not by a `SHA256SUMS` entry

## Context and Problem Statement

In-app updates on the Android client fail on `v0.16.0`. The tile detects the
release, and pressing update reports:

```text
no checksum entry for magic-cli-remote-v0.16.0-arm64.apk
```

That string is
`apps/mobile/lib/data/update/app_update.dart:116`. It is raised when
`sha256For()` (`:183`) finds no line for the APK's basename in the
`SHA256SUMS` asset the phone downloaded.

**The APK genuinely has no line in that file.** Fetched from the live release:

```text
$ gh release download v0.16.0 -p SHA256SUMS && cat SHA256SUMS
dede3ff6…  mcremote-darwin-arm64
24513396…  mcremote-linux-amd64
2f3592aa…  mcremote-linux-arm64
4263856e…  mcremote-windows-amd64.exe
a5562f70…  mcrelay-darwin-arm64
8eb28006…  mcrelay-linux-amd64
2fdebf6c…  mcrelay-linux-arm64
8b2c4f2b…  mcrelay-windows-amd64.exe
```

Eight Go binaries, no APK. The previous release listed it:

```text
$ gh release download v0.15.3 -p SHA256SUMS && cat SHA256SUMS
dfa678ae55cbb78e307a9d07f79794f307c3a1b8227a90921c99c61f77c0b4d7  magic-cli-remote-v0.15.3-arm64.apk
72261901…  mcremote-darwin-arm64-0.15.3.1
…
```

So this is a regression introduced between `v0.15.3` and `v0.16.0`, and it is
a release-side regression, not a client one — `app_update.dart` is unchanged
across that window.

### Where the entry was lost

Commit `32510a6` ("feat(release): adopt MADR 0005 canonical structure and
publishing", 2026-09-02) replaced the release job's hand-rolled staging with
mcplib's reusable publish workflow. The removed code seeded its asset list with
the APK and then hashed the whole list:

```bash
ASSETS=("$APK_OUT")                     # magic-cli-remote-<tag>-arm64.apk
for f in go-bin/mcremote-* go-bin/mcrelay-*; do … ASSETS+=("$out"); done
# "Regenerate over the PUBLISHED names. … Covers the APK too."
sha256sum "${ASSETS[@]}" > "SHA256SUMS-${VER}"
cp -f "$SUMS" SHA256SUMS
```

The replacement copies the build job's manifest through untouched
(`.github/workflows/ci.yml:830`), and that manifest is built over the Go
binaries alone (`:226`):

```bash
(cd dist && sha256sum mcremote-* mcrelay-* > SHA256SUMS)
```

The APK is still staged and still published — it is passed as an extra asset
(`:895`) — but nothing hashes it any more. The comment "Covers the APK too"
went out with the code that made it true.

### Why the manifest cannot simply be widened again

mcplib's verifier, which now gates every publish, requires the manifest to
list the canonical binaries and nothing else
(`mcplib/scripts/verify-selfupdate-release.sh:191-193`):

```python
sums = parse_sums(os.path.join(dirpath, "SHA256SUMS"))
if set(sums) != set(canonical):
    fail("SHA256SUMS must contain exactly the canonical binaries")
```

and it refuses any extra asset whose name is or begins with `SHA256SUMS`
(`:114`), so a second manifest for the APK cannot be declared either. Adding
the APK back to `SHA256SUMS` from this repository alone would fail the publish
job outright.

### Why fixing the release pipeline does not fix `v0.16.0`

`v0.16.0` is already published and immutable:

```text
$ gh release view v0.16.0 --json isImmutable --jq .isImmutable
true
```

Its assets can never be amended. Any repair that lives in CI takes effect
only from the next tag, which leaves the current release permanently
un-updatable by every phone in the field.

### The material that is already there

GitHub serves a server-computed SHA-256 for every release asset in the same
API response the phone already fetches:

```text
$ gh api repos/maccavelli/magic-cli-remote/releases/latest \
    --jq '.assets[]|select(.name|endswith(".apk"))|{name,digest,size}'
{"digest":"sha256:ab00c404a1d6d02e20417d4874eb29207645dae7a883194ce871efabeb54223d",
 "name":"magic-cli-remote-v0.16.0-arm64.apk","size":41092083}
```

Downloaded and hashed on this host, the published APK is
`ab00c404a1d6d02e20417d4874eb29207645dae7a883194ce871efabeb54223d` — the
digest is correct. Cross-checked one release back, `v0.15.3`'s APK digest is
`dfa678ae55cbb78e307a9d07f79794f307c3a1b8227a90921c99c61f77c0b4d7`, which is
byte-for-byte the line that release's `SHA256SUMS` carries. Where the two
sources overlap they agree.

### A second, latent defect in the same path

`_pickSums` (`:168`) returns the **first** asset whose name starts with
`SHA256SUMS`. `v0.16.0` publishes two — `SHA256SUMS` and the bridge's
`SHA256SUMS-0.16.0` — so which one the phone verifies against depends on GitHub's
asset ordering. Neither lists the APK, so it is masked by the failure above
rather than being a separate symptom today.

## Decision Drivers

* **`v0.16.0` is immutable.** A fix that only changes CI cannot repair the
  release that is broken right now.
* **The publish contract is not this repository's to relax.**
  `verify-selfupdate-release.sh` lives in mcplib and is shared by six products.
* **No weaker verification.** The phone installs what it downloads; a mismatch
  must still abort and delete the file.
* **No new trust root.** Whatever the APK is checked against must arrive over
  the same channel, with the same authority, as the file it checks.
* **Recurrence should be impossible by construction**, not prevented by a
  comment or a reviewer noticing.

## Considered Options

* Verify the APK against GitHub's per-asset `digest` field, keeping the
  `SHA256SUMS` lookup as a fallback
* Publish a `magic-cli-remote-<tag>-arm64.apk.sha256` sidecar as an extra asset
* Widen mcplib's release contract so `SHA256SUMS` covers declared extras

## Decision Outcome

Chosen option: "Verify the APK against GitHub's per-asset `digest` field,
keeping the `SHA256SUMS` lookup as a fallback", because it is the only option
that repairs the already-immutable `v0.16.0`, it needs no change to the release
pipeline or to the shared mcplib contract, and it ends the client's dependence
on a manifest that the release contract explicitly forbids from listing the APK.

The `digest` field is read from the release JSON the phone already fetches in
`checkLatest()`, over the same `api.github.com` TLS connection that supplies the
download URL. When it is absent the client falls back to today's `SHA256SUMS`
lookup; when neither yields an expected hash it fails closed exactly as it does
now.

### Consequences

* Good, because `v0.16.0` becomes updatable without republishing anything — the
  digest is already served for it, and verified correct above.
* Good, because it removes the client's coupling to a file whose contents are
  governed by another repository's contract; the class of failure cannot recur.
* Good, because the `SHA256SUMS` fallback keeps releases that predate the
  `digest` field working, and keeps the failure-closed behaviour on a mismatch.
* Good, because the ambiguous `_pickSums` choice stops mattering on the primary
  path, and is tightened on the fallback path in the same change.
* Bad, because the currently installed build cannot use a fix it does not yet
  contain: one APK must be sideloaded by hand to break the chicken-and-egg. Every
  update after that is in-app again.
* Bad, because the expected hash now comes from GitHub's metadata rather than
  from the build. This is not a reduction in trust — `SHA256SUMS` is an
  unsigned asset of the same release, served by the same host — but it does mean
  neither source is a build-provenance check. Attestations are published
  (`actions/attest-build-provenance` in mcplib's publish workflow); using them is
  a strictly stronger future step and is out of scope here.
* Neutral, because the release pipeline is untouched, so nothing about the Go
  self-update path, the installers, or the `v0.16.0` bridge changes.

### Confirmation

* A unit test asserts the digest path is preferred, and a second asserts the
  `SHA256SUMS` fallback still works when `digest` is absent.
* A unit test asserts a **mismatched** digest aborts and deletes the partial
  file — run first against the unfixed code to watch it fail, per
  `AGENTS.md`/global rule "a check is not trusted until it has been seen to
  fail".
* A regression test replays the real `v0.16.0` asset list — an APK, two
  `SHA256SUMS*` assets, neither listing the APK — and asserts verification
  succeeds. This test fails on today's code with the exact reported string.
* End-to-end: a build carrying the fix, sideloaded on the Android device,
  downloads, verifies and installs the live `v0.16.0` APK.

## Pros and Cons of the Options

### Verify the APK against GitHub's per-asset `digest` field, keeping the `SHA256SUMS` lookup as a fallback

* Good, because it fixes the broken, immutable release rather than only the
  next one.
* Good, because it is confined to one Dart file and its tests; CI, mcplib and
  the Go update path are untouched.
* Good, because the value is already in the response the client parses, so the
  fix costs no additional request.
* Neutral, because it makes the phone's verification differ from the CLI's,
  which continues to use `SHA256SUMS` — appropriate, since the APK is the one
  published artifact the manifest is contractually forbidden to cover.
* Bad, because it depends on a GitHub API field rather than on a file the build
  produced.

### Publish a `magic-cli-remote-<tag>-arm64.apk.sha256` sidecar as an extra asset

* Good, because the expected hash would again be computed by the build, from the
  exact bytes being published.
* Good, because a sidecar is a legal extra under mcplib's verifier — the ban is
  only on names beginning `SHA256SUMS`.
* Bad, because it does nothing for `v0.16.0`; the release is immutable and has
  no sidecar, so the phone stays stuck until a `v0.16.1` ships.
* Bad, because it needs a coordinated client and CI change plus a new release
  before any user is unblocked.
* Bad, because it adds a published artifact whose only consumer is one client,
  and a new way for the pair to drift out of step.

### Widen mcplib's release contract so `SHA256SUMS` covers declared extras

* Good, because it restores the pre-`32510a6` behaviour, where one manifest
  covered everything published.
* Bad, because it does nothing for `v0.16.0`, for the same immutability reason.
* Bad, because it changes a contract shared by six products to serve one
  client, and the "exactly the canonical binaries" assertion is deliberate — it
  is what lets a self-updating client discover the canonical set from the
  manifest alone.
* Bad, because it is a cross-repository change requiring an mcplib release, a
  dependency bump here, and a new tag, before anyone is unblocked.

## More Information

* Symptom and fallback: `apps/mobile/lib/data/update/app_update.dart:91-121`,
  `:154-181`.
* Gate that reports "No APK asset on this release" when `sums` is null:
  `apps/mobile/lib/features/settings/app_update_tile.dart:87`. It must stop
  requiring `sums` once the digest path lands.
* Manifest construction: `.github/workflows/ci.yml:226` (build) and `:830`
  (staging); the APK as an extra asset at `:895`.
* Shared contract: `mcplib/scripts/verify-selfupdate-release.sh:114`, `:191-193`
  (mcplib v1.4.1, `8d4d89f`).
* Regression commit: `32510a6`.
* Implementation: [0132-PLAN-verify-the-apk-by-github-asset-digest.md](0132-PLAN-verify-the-apk-by-github-asset-digest.md).
