# Kilo CLI spike — 7.4.22

Live-probed 2026-08-14 on this host for
[0088-MADR-kilo-7.4.22-surface-parity.md](../0088-MADR-kilo-7.4.22-surface-parity.md).
The 7.4.20 corpus in [../kilo-spike-7.4.20/](../kilo-spike-7.4.20/) stays as
historical evidence; do not delete it.

## How

```bash
kilo serve --hostname 127.0.0.1 --port <p> --pure
curl http://127.0.0.1:<p>/global/health
curl http://127.0.0.1:<p>/doc          # OpenAPI 3.1; path names only committed
```

Do not commit `GET /provider` (multi-MB).

## Key results

| Item | Result |
| --- | --- |
| Health | `{"healthy":true,"version":"7.4.22"}` |
| OpenAPI | 3.1.0, title `kilo`, **256** paths (**+13 / −0** vs 7.4.20's 243), **681** schemas |
| Agents | 10 — see `agents-summary.json` |
| Built-in commands | `init`, **`review`**, **`resume-claude`**, **`resume-codex`** — see `commands.json` |
| `code` tools | `bash`, `read`, `glob`, **`grep`**, `edit`, `write`, `task`, `webfetch`, `todowrite`, `websearch`, `skill`, `question`, `suggest`, `kilo_local_recall`, `background_process`, `interactive_terminal`, `plan_exit` |
| Sandbox | unavailable here (`No usable Bubblewrap executable is available`) |
| `allow-everything` | 401 without Basic; 200 with `-u kilo:` |
| Permission reply | 404 without Basic (not gated) |
| Default model | `kilo` → `kilo-auto/free` logged out; `kilo-auto/balanced` with Gateway OAuth |

Added paths (vs `../kilo-spike-7.4.20/openapi-paths.txt`):

```
GET  /api/session/active
POST /api/session/{sessionID}/agent
GET  /api/session/{sessionID}/event
GET  /api/session/{sessionID}/history
POST /api/session/{sessionID}/interrupt
GET  /api/session/{sessionID}/message/{messageID}
POST /api/session/{sessionID}/model
GET  /api/session/{sessionID}/permission/{requestID}
POST /api/session/{sessionID}/revert/clear
POST /api/session/{sessionID}/revert/commit
POST /api/session/{sessionID}/revert/stage
GET  /kilocode/command/files
POST /kilocode/command/remove
```

## Successful turns

2026-08-15, Gateway OAuth, engine default `kilo/kilo-auto/balanced`:

- `TestLivePromptStream` — streamed PONG
- `TestLivePermissionRoundTrip` — bash `echo permission-ok` asked and answered
- `TestLiveToolStreamDynamics` — bash loop, 13 raw tool frames, 1 call
