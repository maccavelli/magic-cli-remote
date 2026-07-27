# Markdownlint assessment and recommendation

Evaluated: 2026-07-27

## Decision

Adopt [`markdownlint-cli2`](https://github.com/DavidAnson/markdownlint-cli2)
with a root [`.markdownlint-cli2.jsonc`](#recommended-configuration) that
explicitly selects the human-facing docs. Start its CI/pre-commit gate only
after the existing baseline is corrected.

Use an inclusion list, not a blanket `**/*.md` pattern plus exclusions. This
repository deliberately keeps human documentation beside MADRs, implementation
plans, and agent instructions. An allow-list makes the boundary reviewable and
prevents a new agent-oriented file from becoming a CI concern by accident.

## Standards and rationale

Markdown is source text as well as rendered output, so the useful rules are
ones that protect both: unambiguous CommonMark structure, readable source, and
reliable GitHub rendering.

- [CommonMark 0.31.2](https://spec.commonmark.org/spec) defines the portable
  block structure for headings, paragraphs, lists, quotes, and fenced code.
- [GitHub's formatting guide](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax)
  documents the GFM features this repository uses, including headings, fenced
  code, tables, and relative links.
- [`markdownlint`](https://github.com/DavidAnson/markdownlint) is a maintained
  CommonMark/GFM linter. Its rules are structural/style checks, not a prose,
  accessibility, spelling, or link-validation substitute.
- [`markdownlint-cli2`](https://github.com/DavidAnson/markdownlint-cli2)
  supports repository-owned JSONC configuration, explicit globs, `--fix`, and
  the same configuration family as the VS Code extension.

## Scope

Lint these 11 docs:

- `README.md`
- `apps/mobile/README.md`
- `docs/chat-performance.md`
- `docs/config.md`
- `docs/config-mcrelay.md`
- `docs/headscale.md`
- `docs/iam-route53-acme.md`
- `docs/mobile-profiling.md`
- `docs/ops-mcrelay.md`
- `docs/protocol-v1.md`
- `docs/tls-letsencrypt.md`

Do not lint `AGENTS.md`, MADRs (`docs/00NN-*.md`), implementation plans, or
other agent-oriented assessments. If a new reader-facing runbook/reference is
added, add it to the configuration in the same change. Longer-term, moving
reader-facing documents to a dedicated directory would make scope selection
simpler, but is not needed for this adoption.

## Audit result

There is no existing `.markdownlint*` configuration, Markdown lint command,
or Markdown CI/pre-commit check. The repository already pins Node 24 in CI, so
the chosen CLI fits the existing toolchain without affecting Go or Flutter
builds.

I ran `markdownlint-cli2` 0.23.2 (markdownlint 0.41.1) on the 11 files above.
The all-default configuration reports 891 findings, mostly its 80-character
line limit and a table-pipe alignment preference. That result is not a useful
baseline: tables intentionally carry paths, flags, keys, and URLs, and 80
columns would cause high-churn prose reflow.

The recommended configuration reports 55 findings in 9 files:

| Finding | Count | Meaning |
| --- | ---: | --- |
| MD013 line length | 48 | Prose above 120 columns; code blocks, tables, and headings are exempt. |
| MD029 ordered-list sequence | 3 | `docs/headscale.md` repeats `3.` where items 4–6 are intended. |
| MD010 hard tab | 1 | One tab in `docs/iam-route53-acme.md`. |
| MD040 fence language | 1 | One unlabelled fence in `docs/protocol-v1.md`. |
| MD004/MD032 list style/spacing | 2 | A `+` list that is not separated from its preceding text in `README.md`. |

These are a small, worthwhile baseline-cleanup set. Fix them before enabling
the gate; do not weaken the structural rules to preserve existing defects.

## Recommended configuration

Create `.markdownlint-cli2.jsonc` in the repository root:

```jsonc
{
  // Explicit scope: reader/operator/developer documentation only.
  "globs": [
    "README.md",
    "apps/mobile/README.md",
    "docs/{chat-performance,config,config-mcrelay,headscale,iam-route53-acme,mobile-profiling,ops-mcrelay,protocol-v1,tls-letsencrypt}.md"
  ],

  "config": {
    // Start from markdownlint's maintained CommonMark/GFM rule set.
    "default": true,

    // Readable prose, without corrupting commands, tables, URLs, or titles.
    "MD013": {
      "line_length": 120,
      "code_blocks": false,
      "tables": false,
      "headings": false,
      "stern": false
    },

    // A repeated label is fine in separate sections, but not among siblings.
    "MD024": { "siblings_only": true },

    // The README deliberately uses bold numbered step lead-ins. Keep this
    // rule off unless those lead-ins are changed to real headings/lists.
    "MD036": false,

    // Make source consistent where the repository already has a convention.
    "MD046": { "style": "fenced" },
    "MD048": { "style": "backtick" },

    // Pipe alignment has no rendered or accessibility benefit and causes
    // noisy diffs when a cell changes. Retain GFM table parsing, not alignment.
    "MD060": false
  }
}
```

All other default rules remain enabled. In particular this enforces heading
hierarchy and blank lines, one H1, ATX heading spacing, consistent list
markers/indentation, no hard tabs/trailing whitespace, labelled fenced code,
valid link syntax, and a final newline. `MD026` (heading punctuation), `MD033`
(inline HTML), and `MD041` (first line H1) work against the current scoped
files and should stay enabled rather than being pre-emptively relaxed.

### Intentional exceptions

`MD013` is set to 120 columns because source readability matters, while
preserving copy-pasteable commands and comprehensible reference tables. Its
`stern: false` setting avoids flagging an otherwise readable line solely for
an unbreakable token such as a URL or identifier.

`MD036` remains disabled for the existing bold step labels; it would otherwise
turn a style choice into recurring false positives. `MD060` is disabled because
both compact and aligned tables render identically, and alignment churn is not
a documentation-quality signal. No broad HTML exception is needed: placeholder
forms such as `<data_dir>` are accepted and no actual inline HTML is used.

## Rollout

1. Add the configuration and fix the 55 baseline findings, using
   `markdownlint-cli2 --fix` only after reviewing its diff.
2. Add a `make markdownlint` target that invokes a pinned CLI version, for
   example:

   ```makefile
   markdownlint:
	 npx --yes --package=markdownlint-cli2@0.23.2 markdownlint-cli2
   ```

3. Run `make markdownlint` in GitHub Actions and in the existing pre-commit
   hook when scoped Markdown files are staged. The configuration's globs make
   the command safe to invoke without shell-dependent file globs.
4. Keep link checking and spelling as separate future decisions. They require
   network/allow-list and vocabulary policies that markdownlint cannot supply.

Pin the CLI version in the Make target (or a small committed Node dev-tools
manifest) rather than using an unversioned `npx` download. Update it
deliberately with the configuration and baseline rechecked.

## Non-goals

This configuration deliberately does not prescribe document templates,
sentence voice, product-name casing, link reachability, or prose quality. It
keeps Markdown structurally valid and consistently readable; editorial review
continues to decide whether a document explains the right thing.
