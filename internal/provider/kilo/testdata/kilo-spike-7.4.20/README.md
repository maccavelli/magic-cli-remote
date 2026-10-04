# Kilo CLI spike — 7.4.20

Live-probed 2026-08-06 on this host for MADR 0075.

## How

```bash
export KILO_SERVER_PASSWORD=...
kilo serve --hostname 127.0.0.1 --port 18765
curl -u kilo:$KILO_SERVER_PASSWORD http://127.0.0.1:18765/global/health
```

## Key results

See `summary.json`. Full OpenAPI was downloaded from `GET /doc` (OpenAPI 3.1, 243 paths) then reduced to:

- `openapi-paths.txt` / `openapi-methods.tsv` / `openapi-schema-names.txt`

Provider catalog (~4.7 MB live) reduced to `provider-summary.json`.

## Successful turn

`POST /session/{id}/prompt_async` with model `{providerID: openrouter, modelID: openrouter/free}` returned **204**; SSE delivered assistant text **PONG** via `message.part.updated` / `message.part.delta`.
