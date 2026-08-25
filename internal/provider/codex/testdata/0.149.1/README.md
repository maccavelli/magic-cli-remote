# Codex 0.149.1 contract evidence

This directory pins the executable contract used by Plan 0109 P1.

- Version: `codex-cli 0.149.1`
- Resolved executable SHA-256:
  `73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`
- Stable counts: 95 client requests, 75 server notifications, 10 server
  requests.
- Experimental counts: 150 client requests, 75 server notifications, 11
  server requests.
- Source-watch commit: `6143217c6730e147f4a1a5a3405d10f580fe9244`.

`manifest.json` is the compact installed-binary inventory and capability
allowlist. `fixtures.json` records sanitized schema identities and required
field names without live paths, prompts, account data, or credentials.
`source-watch-manifest.json` is separate leading evidence and never enables a
runtime capability.

Regenerate the installed schemas only into a temporary directory:

```sh
codex_contract_dir="$(mktemp -d)"
codex app-server generate-json-schema --out "$codex_contract_dir/stable"
codex app-server generate-json-schema --experimental \
  --out "$codex_contract_dir/experimental"
```

Extract the pinned source exports into a second temporary directory, set
`CODEX_CONTRACT_STABLE_SCHEMA`, `CODEX_CONTRACT_EXPERIMENTAL_SCHEMA`,
`CODEX_SOURCE_STABLE_SCHEMA`, and `CODEX_SOURCE_EXPERIMENTAL_SCHEMA` to the
four `codex_app_server_protocol.v2.schemas.json` files, then run:

```sh
CODEX_CONTRACT_GENERATE=1 go test ./internal/provider/codex \
  -run '^TestGenerateContractManifest$' -count=1 -v
```

The `ServerRequest.json` export must sit beside each composite schema. Review
the generated diff, verify the binary digest independently, and remove both
temporary directories. The no-model drift and catalog probe is:

```sh
make live-codex-contract
```
