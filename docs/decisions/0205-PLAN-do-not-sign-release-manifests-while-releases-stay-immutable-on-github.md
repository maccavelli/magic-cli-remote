---
status: proposed
date: 2026-10-20
associated-madr: "0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement not signing release manifests, with release tags limited to the owner

Associated MADR: [0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md)

## Goal

Only the owner can create, move or delete a `v*` tag in this repository, and releases still publish
as before. Nothing is signed.

## Scope

* One repository ruleset, a GitHub setting. No file in the tree changes, and no workflow.
* The pointer in 0054's S9-4, written with this record.

What a release needs from tags, read 2026-10-20: the publish job creates the release with
`gh release create "$TAG" --verify-tag` (library `publish-selfupdate-release.yml:358`, `:371`), so it
never creates a tag. The only workflow here that pushes is the flake ledger, to `master`
(`ci-flake-ledger.yml:141`).

## Implementation Steps

Each step changes GitHub settings or pushes a tag, so each runs only on the owner's explicit ask in
that turn. The owner may make the settings change in the web UI instead.

1. **Show that a tag ruleset is enforced here, failing first.**
   - Create a probe ruleset with no bypass, on `refs/tags/ruleset-probe-*`, blocking creation.
   - Push the tag `ruleset-probe-1`. It must be rejected.
   - Delete the probe ruleset. No probe tag exists to clean up.
2. **Create the ruleset** `Release tags`:

   ```json
   {
     "name": "Release tags",
     "target": "tag",
     "enforcement": "active",
     "conditions": {"ref_name": {"include": ["refs/tags/v*"], "exclude": []}},
     "rules": [{"type": "creation"}, {"type": "update"}, {"type": "deletion"}, {"type": "non_fast_forward"}],
     "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}]
   }
   ```

   Role 5 is the repository's admin role, which the owner holds.
3. **Read it back** from `repos/…/rulesets/<id>`, and record the rules, the condition and the bypass
   list as returned.

## Verification

* Step 1's probe rejection, recorded with GitHub's message.
* Step 3's read-back matches step 2.
* The next release tag, pushed by the owner (PLAN 0054 S10b), is accepted, and its release
  publishes.
* What cannot be shown with one account: that a non-admin is refused on `v*`. Step 1 shows the
  mechanism on another pattern instead.

## Rollout and Rollback

Rollback is deleting the ruleset. Nothing in the field depends on it.

## Execution record
