# Android release signing — one-time setup runbook

Companion to MADR 0065 (B1). After this runbook is complete, every tagged
release ships an APK signed with one stable upload key, which is the
prerequisite for in-place phone updates, Play Protect reputation, and
package-name registration under the verified developer account.

The CI side is already wired (`.github/workflows/ci.yml`, android-apk job):
on the **canonical repo** it fails any tag build until the four secrets
below exist, provisions `key.properties` from them, and asserts post-build
that the APK is signed by the upload key — with an optional pinned
certificate digest.

## Forks and clones

Signing is the canonical repo's release discipline, not a demand on anyone
who forks. The workflow separates the concerns:

- **No signing secrets in a fork** → tag builds proceed **unsigned**
  (debug key) with a log note. The APK works, but cannot update an
  installed app — fine for personal use.
- **A fork that sets all four secrets** (its own keystore) → gets the full
  signed path, identical to canonical.
- **Partial secrets** → fails in any repo: that is misconfiguration, not a
  fork.
- Only the canonical repo (`maccavelli/magic-cli-remote`) hard-fails on
  missing secrets — its releases must never silently ship debug-signed.

## 1. Generate the keystore (once, on a trusted machine)

```bash
keytool -genkeypair -v \
  -keystore ~/mcremote-upload.jks \
  -storetype PKCS12 \
  -keyalg RSA -keysize 4096 \
  -validity 10950 \
  -alias upload \
  -dname "CN=Magic CLI Remote, O=maccavelli"
```

Notes:

- **PKCS12** means the store password and key password are the same value —
  set both secrets identically in step 3.
- Pick a password **without backslashes** (`\`): the CI step writes it into
  a Java `.properties` file, where backslashes are escape characters.
- 10950 days ≈ 30 years. Android requires validity beyond 2033-10-22 for
  Play; long validity is standard for upload keys.
- `keytool` ships with any JDK (`brew install temurin` if absent).

## 2. Back it up before anything else touches it

Losing this keystore permanently recreates the update-impossible situation
B1 describes — there is no recovery. Before step 3:

- Copy `mcremote-upload.jks` to at least one **offline** location
  (password manager attachment, encrypted USB, printed recovery — your
  call, but not only this laptop).
- Store the password in the password manager alongside it.
- Do **not** commit it anywhere; `**/*.jks` and `key.properties` are
  already gitignored under `apps/mobile/android/`.

## 3. Set the GitHub Actions secrets

```bash
cd ~/gitrepos/go/magic-cli-remote
base64 -i ~/mcremote-upload.jks | gh secret set ANDROID_KEYSTORE_BASE64
gh secret set ANDROID_KEYSTORE_PASSWORD   # paste the password at the prompt
gh secret set ANDROID_KEY_PASSWORD        # same value (PKCS12)
gh secret set ANDROID_KEY_ALIAS --body upload
```

## 4. First signed release, and pinning the certificate

1. Tag the next release as usual. The android-apk job now signs with the
   upload key; the **"Assert release signature"** step prints the signer's
   SHA-256 digest and notes it is unpinned.
2. Copy the digest from that log and pin it:

   ```bash
   gh variable set ANDROID_RELEASE_CERT_SHA256 --body <hex-digest>
   ```

   From then on, any build whose signer differs — wrong keystore, tampered
   secret, debug fallback — fails CI.

## 5. Device migration (one-time, per existing install)

An installed dev-key APK cannot update to the upload-key APK. On each
existing device, once:

1. Uninstall the app (this wipes the stored pairing — expected).
2. Install the signed release APK.
3. Re-pair (`mcremote pair code --name <device>`).
4. On the host, clean up the orphaned old device row:
   `mcremote pair list` → `mcremote pair revoke <id>` (or `pair prune`).

Local development is unaffected: without `key.properties`, local builds
keep using the machine's debug key, and `flutter install` over a dev-key
install keeps working.

Post-incident notes (MADR 0066, 2026-08-02): an in-place update is
expected to **preserve pairing** from the first post-0066 release onward
(hardware rows E1/E2 in
[ops-hardware-validation.md](ops-hardware-validation.md)). If Android's
keystore still resets the secret store across an update, the app now shows
one "Stored credentials were reset" banner and recovers with a re-pair —
preferences and pinned paths survive; never clear app data for this. After
any re-enrolment, remove the orphaned old device row on the host:
`mcremote pair list` (its KEY column shows each device's enrolled key
prefix) → `mcremote pair revoke <id>`, or `mcremote pair prune`.

## 6. Register the package name (closes MADR 0065 B3)

With the first signed APK in hand, register
`com.maccavelli.magic_cli_remote` in the verified developer console,
proving ownership with the upload-key-signed APK. This retires the
"unverified developer" install surface ahead of the 2026-09-30 enforcement
wave (MADR 0065 §2.2).

Console choices made during registration (2026-08-02), and why:

- **Automatic protection: OFF.** It wraps the artifact with an
  installed-from-Play check — the exact opposite of a sideload-distributed
  app. Integrity here comes from the stable key, the CI digest pin, and
  release checksums instead.
- **The three creation declarations (program policies, Play App Signing,
  export laws): accepted.** All three govern the *Play distribution
  channel*, which this app does not use. Play App Signing in particular
  touches only Play-delivered artifacts — the GitHub APK stays signed by
  the upload key. Standing caveat: if this package were ever actually
  *published* through Play, Google would sign Play copies with its own
  held key, Play and GitHub installs could not update each other, and the
  self-updater would violate Play policy — publishing there is a separate
  decision requiring its own MADR, not a checkbox.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Tag build fails at "Provision release signing" | One of the four secrets missing or empty — step 3 |
| `keytool` verification fails in the same step | Wrong password or alias in the secrets; the keystore itself decoded fine |
| "APK is debug-signed" assertion failure | `key.properties` was written but Gradle fell back — usually a malformed value (backslash in the password) |
| "signer digest does not match" | The keystore behind the secret changed. If deliberate (rotation), update `ANDROID_RELEASE_CERT_SHA256`; if not, treat as compromise |
