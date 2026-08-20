# Kilo CLI spike — 7.4.23

Captured on 2026-08-20 for
[0108-MADR-kilo-7.4.23-surface-parity.md](../0108-MADR-kilo-7.4.23-surface-parity.md).
The 7.4.20 and 7.4.22 corpora remain historical evidence.

## Provenance

| Item | Pinned value |
| --- | --- |
| Installed CLI | Kilo 7.4.23 |
| Published tag | `v7.4.23`, commit `40fa10e50a75c4887978d892520d1246515413bf` |
| 7.4.23 functional source boundary | `0d5d334480bc2093a12b27a34b03cac88cf33422` |
| Comparison release | `v7.4.22`, commit `67cda85c94937a7dfad68993bdddc76cb0353c36` |
| Runtime health | healthy, version 7.4.23 |
| Runtime OpenAPI | 255 paths, 680 schemas, 119 canonical Event types |
| ACP initialize | protocol 1, Kilo agent 7.4.23 |

The published tag commit contains release/version generation. Source comparisons
use its functional parent rather than a mutable checkout HEAD.

## Isolation

Every runtime probe used a fresh temporary working directory, home, and
`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and
`XDG_CACHE_HOME`. The environment also set:

```text
KILO_CONFIG_CONTENT={"permission":{"*":"allow"}}
KILO_AUTH_CONTENT={}
KILO_DISABLE_PROJECT_CONFIG=1
KILO_DISABLE_AUTOUPDATE=1
KILO_DISABLE_AUTOCOMPACT=1
KILO_DISABLE_MODELS_FETCH=1
KILO_PURE=1
```

The live server command was:

```bash
kilo serve --hostname 127.0.0.1 --port 0 --pure
```

Port zero means “prefer 4096, then request an available port.” This capture
selected 4096, but the probe and live test parse the emitted URL and do not
treat that port as invariant.

## Reproduction

Run the deterministic, no-model acceptance gates:

```bash
go test -tags live_kilo ./internal/provider/kilo/ \
  -run 'TestLiveKilo7423(Surface|ACPInitialize|ReadOnlyAgentBoundaries)$' \
  -count=1 -timeout 180s -v
```

Extract source documents from the pinned boundaries:

```bash
git -C /path/to/kilocode show \
  67cda85c94937a7dfad68993bdddc76cb0353c36:packages/sdk/openapi.json
git -C /path/to/kilocode show \
  0d5d334480bc2093a12b27a34b03cac88cf33422:packages/sdk/openapi.json
```

Canonical Event extraction resolves each `$ref` in
`components.schemas.Event.anyOf` and reads only the referenced schema's
top-level `properties.type.enum`. Recursive scans are not canonical because
they include nested memory values such as `recalled`, `saved`, and
`skipped`.

The ACP fixture comes from this single NDJSON request:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"mcremote-0108-probe","version":"1"}}}
```

A fresh state may print migration chatter before the response. The parser
ignores non-JSON lines, requires exactly one matching JSON-RPC response, and
commits only its normalized `result`. Agent boundaries come from
`kilo debug agent code`, `ask`, and `plan`, evaluated with Kilo's
last-matching-rule semantics.

## Exact release delta

| Surface | 7.4.22 | 7.4.23 | Delta |
| --- | ---: | ---: | --- |
| OpenAPI paths | 256 | 255 | −1 |
| OpenAPI schemas | 681 | 680 | −1 |
| Canonical Event types | 119 | 119 | 0 |

The only removed path is `/kilocode/agent/requirements`. The only removed
schema is `AgentRequirementResult`. No paths, schemas, or Event types were
added, and the Event-type set is unchanged.

## Files

* `openapi-paths.txt` — sorted 255-path runtime snapshot.
* `event-types.txt` — sorted 119-type canonical Event snapshot.
* `openapi-summary.json` — pinned source boundaries, counts, and exact sets.
* `agents-summary.json` — stable agent identity, mode, visibility, and native
  fields.
* `agent-permission-summary.json` — controlled Code/Ask/Plan effective
  actions under a global allow.
* `commands.json` — stable built-ins only.
* `acp-initialize.json` — normalized ACP initialize result.

## Sanitization and environmental observations

The corpus excludes credentials, providers, model catalogs, raw OpenAPI,
server logs, timestamps, absolute home-state paths, transport identifiers,
and project/MCP/skill-backed commands. ACP auth methods retain only the stable
`kilo-login` identifier.

The runtime exposed ten native agents: five visible primary agents, three
hidden primary agents, and two subagents. Its stable built-ins were
`init`, `review`, `resume-claude`, and `resume-codex`.
Environment-backed commands such as `kilo-config` were deliberately
excluded. Model/provider availability remains account-dependent and is not a
7.4.23 release invariant.
