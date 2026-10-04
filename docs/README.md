# Docs

Index for magic-cli-remote. This tree holds only this file, [architecture.md](architecture.md), and three directories:

- [architecture.md](architecture.md) — the system as it is now
- [decisions/](decisions/) — numbered MADRs and PLANs
- [reports/](reports/) — numbered REPORTs
- [guides/](guides/) — unnumbered operator, protocol, and language guides

A second tree covers the Flutter companion:
[apps/mobile/docs/README.md](../apps/mobile/docs/README.md).

Records share one repository-wide `NNNN` sequence and keep the kind infix (`MADR`, `PLAN`, `REPORT`, `GATES`). Next unused number: `python3 scripts/check_records.py --next`.

## Records

<!-- check_records.py ToC begin (generated; do not hand-edit between the markers) -->
| Number | Kind | Title | Record |
| :--- | :--- | :--- | :--- |
| 0001 | MADR | MADR 0001: Architecture for Multi-CLI Remote Control Orchestrator (mcremote) | [0001-MADR-architecture-mcremote.md](decisions/0001-MADR-architecture-mcremote.md) |
| 0002 | MADR | Report: Community Landscape Assessment & Foundation Stack Recommendations for `mcremote` | [0002-MADR-community-assessment-and-stack-recommendations.md](decisions/0002-MADR-community-assessment-and-stack-recommendations.md) |
| 0003 | MADR | MADR 0003: Phase 1 scaffolding decisions | [0003-MADR-phase1-decisions.md](decisions/0003-MADR-phase1-decisions.md) |
| 0004 | MADR | 0004 — Certificate management | [0004-MADR-certificate-management.md](decisions/0004-MADR-certificate-management.md) |
| 0004 | PLAN | Hardening implementation plan | [0004-PLAN-hardening-implementation.md](decisions/0004-PLAN-hardening-implementation.md) |
| 0005 | MADR | 0005 — Client identity | [0005-MADR-client-identity.md](decisions/0005-MADR-client-identity.md) |
| 0006 | MADR | 0006 — Token lifecycle | [0006-MADR-token-lifecycle-decision.md](decisions/0006-MADR-token-lifecycle-decision.md) |
| 0007 | MADR | 0007 — Tailnet lock (control-plane MITM) | [0007-MADR-tailnet-lock-decision.md](decisions/0007-MADR-tailnet-lock-decision.md) |
| 0008 | MADR | 0008 — Headscale-issued certificates | [0008-MADR-headscale-certs-decision.md](decisions/0008-MADR-headscale-certs-decision.md) |
| 0009 | MADR | MADR 0009: Post-hardening action plan | [0009-MADR-post-hardening-action-plan.md](decisions/0009-MADR-post-hardening-action-plan.md) |
| 0010 | MADR | Network stack assessment: mcremote Go server | [0010-MADR-network-stack-assessment.md](decisions/0010-MADR-network-stack-assessment.md) |
| 0011 | MADR | MADR 0011: OpenCode ACP provider | [0011-MADR-opencode-provider-plan.md](decisions/0011-MADR-opencode-provider-plan.md) |
| 0012 | MADR | MADR 0012: mcremote Go daemon assessment & action plan | [0012-MADR-mcremote-daemon-assessment-action-plan.md](decisions/0012-MADR-mcremote-daemon-assessment-action-plan.md) |
| 0013 | MADR | MADR 0013: Daemon audit remediation — decisions & deferral register | [0013-MADR-audit-remediation-decisions.md](decisions/0013-MADR-audit-remediation-decisions.md) |
| 0014 | MADR | MADR 0014: SSE reconnect resync (H4) — engine-state reconciliation for the HTTP transport | [0014-MADR-sse-reconnect-resync-decision.md](decisions/0014-MADR-sse-reconnect-resync-decision.md) |
| 0015 | MADR | MADR 0015: mcrelay — outbound relay transport & security | [0015-MADR-mcrelay-transport-security.md](decisions/0015-MADR-mcrelay-transport-security.md) |
| 0016 | MADR | MADR 0016: mcrelay audit — findings & P1–P6 hardening | [0016-MADR-mcrelay-audit-hardening.md](decisions/0016-MADR-mcrelay-audit-hardening.md) |
| 0017 | MADR | MADR 0017: mcrelay memory / GC / overflow / security action plan | [0017-MADR-mcrelay-memory-security-action-plan.md](decisions/0017-MADR-mcrelay-memory-security-action-plan.md) |
| 0018 | MADR | MADR 0018: Mobile session chat performance, stability & polish | [0018-MADR-mobile-chat-performance-action-plan.md](../apps/mobile/docs/decisions/0018-MADR-mobile-chat-performance-action-plan.md) |
| 0019 | MADR | MADR 0019: OpenCode process management — remove the ACP transport, guarantee a single engine | [0019-MADR-opencode-process-management-plan.md](decisions/0019-MADR-opencode-process-management-plan.md) |
| 0020 | MADR | MADR 0020: OpenCode session tree + async control plane | [0020-MADR-opencode-session-tree.md](decisions/0020-MADR-opencode-session-tree.md) |
| 0021 | MADR | OpenCode HTTP API coverage matrix (mcremote) | [0021-MADR-opencode-http-api-coverage.md](decisions/0021-MADR-opencode-http-api-coverage.md) |
| 0022 | MADR | MADR 0022: Plan mode parity (`/plan`) across providers | [0022-MADR-plan-mode-parity.md](decisions/0022-MADR-plan-mode-parity.md) |
| 0023 | MADR | MADR 0023: A canonical slash-command vocabulary across agent CLIs | [0023-MADR-canonical-slash-commands.md](decisions/0023-MADR-canonical-slash-commands.md) |
| 0023 | REPORT | Agent CLI Commands & ACP Standard Comparison Matrix | [0023-REPORT-agent-cli-slash-commands-matrix.md](../docs/reports/0023-REPORT-agent-cli-slash-commands-matrix.md) |
| 0024 | MADR | MADR 0024: Coalesce streaming chunk text at the transport emit seam | [0024-MADR-stream-coalescing.md](decisions/0024-MADR-stream-coalescing.md) |
| 0025 | MADR | MADR 0025: Goose ACP-over-HTTP provider | [0025-MADR-goose-provider.md](decisions/0025-MADR-goose-provider.md) |
| 0025 | PLAN | Goose ACP-over-HTTP Provider — Implementation Plan | [0025-PLAN-goose-provider.md](decisions/0025-PLAN-goose-provider.md) |
| 0026 | MADR | Mobile App Goose Support — Assessment | [0026-MADR-mobile-goose-support.md](../apps/mobile/docs/decisions/0026-MADR-mobile-goose-support.md) |
| 0027 | MADR | MADR 0027: OpenCode Chat Streaming & Rendering Hardening | [0027-MADR-opencode-streaming-rendering.md](decisions/0027-MADR-opencode-streaming-rendering.md) |
| 0027 | PLAN | OpenCode Streaming & Rendering — Phase 1 Implementation Plan | [0027-PLAN-opencode-streaming-rendering.md](decisions/0027-PLAN-opencode-streaming-rendering.md) |
| 0028 | MADR | MADR 0028: Codex CLI provider — surfaces, protocol, and implementation specifications | [0028-MADR-codex-provider.md](decisions/0028-MADR-codex-provider.md) |
| 0028 | PLAN | Codex CLI provider: implementation plan | [0028-PLAN-codex-provider.md](decisions/0028-PLAN-codex-provider.md) |
| 0029 | MADR | MADR 0029: Canonical provider platform, session surfaces, and retention policy | [0029-MADR-provider-platform-canonicalization.md](decisions/0029-MADR-provider-platform-canonicalization.md) |
| 0029 | PLAN | Provider platform canonicalization: implementation plan | [0029-PLAN-provider-platform-canonicalization.md](decisions/0029-PLAN-provider-platform-canonicalization.md) |
| 0030 | MADR | MADR 0030: Evidence-based Goose remote parity | [0030-MADR-goose-remote-parity.md](decisions/0030-MADR-goose-remote-parity.md) |
| 0030 | PLAN | Goose remote parity: implementation plan | [0030-PLAN-goose-remote-parity.md](decisions/0030-PLAN-goose-remote-parity.md) |
| 0031 | MADR | MADR 0031: OpenCode catalog correctness and bounded session metadata | [0031-MADR-opencode-catalog-and-metadata-parity.md](decisions/0031-MADR-opencode-catalog-and-metadata-parity.md) |
| 0031 | PLAN | OpenCode catalog and metadata parity: implementation plan | [0031-PLAN-opencode-catalog-metadata-parity.md](decisions/0031-PLAN-opencode-catalog-metadata-parity.md) |
| 0032 | MADR | Report 0032: Codex Chat UI/UX — Noise, Correctness, and Hardening | [0032-MADR-codex-ui-ux-polish-report.md](decisions/0032-MADR-codex-ui-ux-polish-report.md) |
| 0033 | MADR | Report 0033: OpenCode Chat UI/UX — Tool Streaming and Coalescing | [0033-MADR-opencode-ui-ux-polish-report.md](decisions/0033-MADR-opencode-ui-ux-polish-report.md) |
| 0034 | MADR | MADR 0034: Tool-stream fidelity — dedup, visible output, and in-place update ordering | [0034-MADR-opencode-tool-stream-fidelity.md](decisions/0034-MADR-opencode-tool-stream-fidelity.md) |
| 0034 | PLAN | OpenCode tool-stream fidelity: implementation plan | [0034-PLAN-opencode-tool-stream-fidelity.md](decisions/0034-PLAN-opencode-tool-stream-fidelity.md) |
| 0035 | MADR | MADR 0035: Codex chat remediation — item-stream fidelity, command truth, and capability disclosure | [0035-MADR-codex-ui-ux-remediation.md](decisions/0035-MADR-codex-ui-ux-remediation.md) |
| 0035 | PLAN | Codex chat UI/UX remediation: implementation plan | [0035-PLAN-codex-ui-ux-remediation.md](decisions/0035-PLAN-codex-ui-ux-remediation.md) |
| 0036 | MADR | MADR 0036: Protocol contract completeness — specify the vocabularies, pair the failure, guard the drift | [0036-MADR-protocol-contract-completeness.md](decisions/0036-MADR-protocol-contract-completeness.md) |
| 0036 | PLAN | Protocol contract completeness: implementation plan | [0036-PLAN-protocol-contract-completeness.md](decisions/0036-PLAN-protocol-contract-completeness.md) |
| 0037 | MADR | MADR 0037: CLI capability uptake — reasoning effort, plugin isolation, and origin alignment | [0037-MADR-cli-capability-uptake.md](decisions/0037-MADR-cli-capability-uptake.md) |
| 0037 | PLAN | CLI capability uptake: implementation plan | [0037-PLAN-cli-capability-uptake.md](decisions/0037-PLAN-cli-capability-uptake.md) |
| 0038 | MADR | Grok Build ACP parity assessment (grok 0.2.112) | [0038-MADR-grok-acp-parity-assessment.md](decisions/0038-MADR-grok-acp-parity-assessment.md) |
| 0039 | MADR | MADR 0039: Grok ACP parity — mid-session model switch, live catalog, and CLI policy surfaces | [0039-MADR-grok-acp-parity.md](decisions/0039-MADR-grok-acp-parity.md) |
| 0039 | PLAN | Grok ACP parity: implementation plan | [0039-PLAN-grok-acp-parity.md](decisions/0039-PLAN-grok-acp-parity.md) |
| 0040 | MADR | Markdownlint assessment and recommendation | [0040-MADR-markdownlint-assessment.md](decisions/0040-MADR-markdownlint-assessment.md) |
| 0041 | MADR | Android app: deep-dive debug audit | [0041-MADR-android-app-debug-audit.md](../apps/mobile/docs/decisions/0041-MADR-android-app-debug-audit.md) |
| 0042 | MADR | MADR 0042: Android app remediation — stable tool rows, coalesced ingest, and the audit backlog | [0042-MADR-android-app-remediation.md](../apps/mobile/docs/decisions/0042-MADR-android-app-remediation.md) |
| 0042 | PLAN | Android app remediation: implementation plan | [0042-PLAN-android-app-remediation.md](../apps/mobile/docs/decisions/0042-PLAN-android-app-remediation.md) |
| 0043 | MADR | MADR 0043: Model selection — scoped catalogs, a provider step, and an in-session picker | [0043-MADR-model-selection.md](decisions/0043-MADR-model-selection.md) |
| 0043 | PLAN | Model selection: implementation plan | [0043-PLAN-model-selection.md](decisions/0043-PLAN-model-selection.md) |
| 0044 | MADR | MADR 0044: Auto-approve as a session mode (OpenCode, Codex) | [0044-MADR-auto-approve-modes.md](decisions/0044-MADR-auto-approve-modes.md) |
| 0044 | PLAN | MADR 0044 — Implementation plan: auto-approve session modes | [0044-PLAN-auto-approve-modes.md](decisions/0044-PLAN-auto-approve-modes.md) |
| 0045 | MADR | MADR 0045: Mobile App Hardening Audit — Findings and Remediation Decisions | [0045-MADR-mobile-app-hardening-audit.md](../apps/mobile/docs/decisions/0045-MADR-mobile-app-hardening-audit.md) |
| 0045 | PLAN | MADR 0045 — Implementation plan: mobile app hardening audit | [0045-PLAN-mobile-app-hardening-audit.md](../apps/mobile/docs/decisions/0045-PLAN-mobile-app-hardening-audit.md) |
| 0046 | MADR | MADR 0046: Post-Remediation Mobile Debug Pass — Findings and Fix Decisions | [0046-MADR-mobile-debug-pass.md](../apps/mobile/docs/decisions/0046-MADR-mobile-debug-pass.md) |
| 0046 | PLAN | MADR 0046 — Implementation plan: post-remediation mobile debug pass | [0046-PLAN-mobile-debug-pass.md](../apps/mobile/docs/decisions/0046-PLAN-mobile-debug-pass.md) |
| 0047 | MADR | MADR 0047: Codex default mode, create-time selection, and auto sandbox | [0047-MADR-codex-default-mode.md](decisions/0047-MADR-codex-default-mode.md) |
| 0047 | PLAN | MADR 0047 — Implementation plan: Codex default mode and auto sandbox | [0047-PLAN-codex-default-mode.md](decisions/0047-PLAN-codex-default-mode.md) |
| 0048 | MADR | MADR 0048: Codex sandbox user-namespace failure — auto cannot write | [0048-MADR-codex-sandbox-namespace.md](decisions/0048-MADR-codex-sandbox-namespace.md) |
| 0048 | PLAN | MADR 0048 — Implementation plan: Codex sandbox namespace / auto-write recovery | [0048-PLAN-codex-sandbox-namespace.md](decisions/0048-PLAN-codex-sandbox-namespace.md) |
| 0049 | MADR | MADR 0049: Grok has no auto mode — per-session auto-approve for ACP-stdio agents | [0049-MADR-grok-auto-mode.md](decisions/0049-MADR-grok-auto-mode.md) |
| 0049 | PLAN | MADR 0049 — Implementation plan: grok auto mode | [0049-PLAN-grok-auto-mode.md](decisions/0049-PLAN-grok-auto-mode.md) |
| 0050 | MADR | MADR 0050: Grok CLI surface drift — seven dead config options | [0050-MADR-grok-cli-surface-drift.md](decisions/0050-MADR-grok-cli-surface-drift.md) |
| 0050 | PLAN | MADR 0050 — Implementation plan: grok CLI surface drift | [0050-PLAN-grok-cli-surface-drift.md](decisions/0050-PLAN-grok-cli-surface-drift.md) |
| 0051 | MADR | MADR 0051: Chat transcript noise — auto-approval notices and sub-agent output | [0051-MADR-auto-approve-chat-noise.md](decisions/0051-MADR-auto-approve-chat-noise.md) |
| 0051 | PLAN | MADR 0051 — Implementation plan: approval-summary cards and sub-agent suppression | [0051-PLAN-auto-approve-chat-noise.md](decisions/0051-PLAN-auto-approve-chat-noise.md) |
| 0052 | MADR | MADR 0052: Per-model thinking levels, and what the settings panel should hold | [0052-MADR-thinking-levels-and-settings.md](decisions/0052-MADR-thinking-levels-and-settings.md) |
| 0052 | PLAN | MADR 0052 — Implementation plan: thinking levels and the settings panel | [0052-PLAN-thinking-levels-and-settings.md](decisions/0052-PLAN-thinking-levels-and-settings.md) |
| 0053 | MADR | MADR 0053: Grok auto mode — silent arm, missing chip, mode-path gaps | [0053-MADR-grok-auto-mode-silent-arm.md](decisions/0053-MADR-grok-auto-mode-silent-arm.md) |
| 0053 | PLAN | MADR 0053 — Implementation plan: grok auto silent arm and mode-path gaps | [0053-PLAN-grok-auto-mode-silent-arm.md](decisions/0053-PLAN-grok-auto-mode-silent-arm.md) |
| 0055 | MADR | Remediate the mcremote Go server from the 2026-07 audit | [0055-MADR-mcremote-server-remediation.md](decisions/0055-MADR-mcremote-server-remediation.md) |
| 0055 | PLAN | mcremote Go server remediation plan | [0055-PLAN-mcremote-server-remediation.md](decisions/0055-PLAN-mcremote-server-remediation.md) |
| 0056 | MADR | MADR 0056: mcremote ↔ Android protocol-stack audit | [0056-MADR-mcremote-android-protocol-stack-audit.md](../apps/mobile/docs/decisions/0056-MADR-mcremote-android-protocol-stack-audit.md) |
| 0056 | PLAN | MADR 0056 — Implementation plan: mcremote ↔ Android protocol-stack remediation | [0056-PLAN-mcremote-android-protocol-stack-remediation.md](../apps/mobile/docs/decisions/0056-PLAN-mcremote-android-protocol-stack-remediation.md) |
| 0057 | MADR | MADR 0057: Chat session markdown streaming — cross-stack re-assessment | [0057-MADR-chat-markdown-stream-hardening.md](decisions/0057-MADR-chat-markdown-stream-hardening.md) |
| 0057 | PLAN | MADR 0057 — Implementation plan: chat markdown + stream hardening | [0057-PLAN-chat-markdown-stream-hardening.md](decisions/0057-PLAN-chat-markdown-stream-hardening.md) |
| 0058 | MADR | MADR 0058: macOS launchd service setup — research, gaps, and hardened design | [0058-MADR-macos-launchd-service-hardening.md](decisions/0058-MADR-macos-launchd-service-hardening.md) |
| 0058 | PLAN | MADR 0058 — Implementation plan: macOS launchd service (user LaunchAgent) | [0058-PLAN-macos-launchd-service-implementation.md](decisions/0058-PLAN-macos-launchd-service-implementation.md) |
| 0059 | MADR | MADR 0059: XDG paths and Linux/macOS functional parity | [0059-MADR-native-paths-and-linux-macos-parity.md](decisions/0059-MADR-native-paths-and-linux-macos-parity.md) |
| 0059 | PLAN | MADR 0059 — Implementation plan: XDG paths and Linux/macOS parity | [0059-PLAN-native-paths-and-linux-macos-parity.md](decisions/0059-PLAN-native-paths-and-linux-macos-parity.md) |
| 0060 | MADR | MADR 0060: Local unsigned build and install on macOS and Linux | [0060-MADR-local-unsigned-build-and-install.md](decisions/0060-MADR-local-unsigned-build-and-install.md) |
| 0060 | PLAN | PLAN 0060: Local unsigned build and install — implementation | [0060-PLAN-local-unsigned-build-and-install.md](decisions/0060-PLAN-local-unsigned-build-and-install.md) |
| 0061 | MADR | MADR 0061: Relay pair advertise vs register, and phone path selection | [0061-MADR-relay-pair-advertise-and-path-selection.md](decisions/0061-MADR-relay-pair-advertise-and-path-selection.md) |
| 0062 | MADR | MADR 0062: Phone transport awareness (mesh / relay selection) | [0062-MADR-phone-transport-selection.md](decisions/0062-MADR-phone-transport-selection.md) |
| 0062 | PLAN | MADR 0062 — Implementation plan: phone transport selection | [0062-PLAN-phone-transport-selection.md](decisions/0062-PLAN-phone-transport-selection.md) |
| 0063 | MADR | MADR 0063: Connection status must be verified, not assumed | [0063-MADR-connection-liveness-truth.md](decisions/0063-MADR-connection-liveness-truth.md) |
| 0063 | PLAN | MADR 0063 — Implementation plan: connection liveness truth | [0063-PLAN-connection-liveness-implementation.md](decisions/0063-PLAN-connection-liveness-implementation.md) |
| 0064 | MADR | MADR 0064: Connect screen — one screen, four steps | [0064-MADR-connect-screen-simplification.md](decisions/0064-MADR-connect-screen-simplification.md) |
| 0064 | PLAN | MADR 0064 — Implementation plan: connect screen simplification | [0064-PLAN-connect-screen-simplification.md](decisions/0064-PLAN-connect-screen-simplification.md) |
| 0065 | MADR | MADR 0065: Update automation — `mcremote update`, `mcrelay update`, phone "Restart to update" | [0065-MADR-update-automation.md](decisions/0065-MADR-update-automation.md) |
| 0065 | PLAN | MADR 0065 — Implementation plan: update automation | [0065-PLAN-update-automation.md](decisions/0065-PLAN-update-automation.md) |
| 0066 | MADR | MADR 0066: Secure-storage upgrade resilience and credential recovery | [0066-MADR-secure-storage-upgrade-resilience.md](decisions/0066-MADR-secure-storage-upgrade-resilience.md) |
| 0066 | PLAN | PLAN 0066: Secure-storage upgrade resilience — implementation plan | [0066-PLAN-secure-storage-upgrade-resilience.md](decisions/0066-PLAN-secure-storage-upgrade-resilience.md) |
| 0067 | MADR | MADR 0067: iOS port of the mobile companion app | [0067-MADR-ios-port.md](../apps/mobile/docs/decisions/0067-MADR-ios-port.md) |
| 0067 | PLAN | MADR 0067 — Implementation plan: iOS port of the mobile companion app | [0067-PLAN-ios-port.md](../apps/mobile/docs/decisions/0067-PLAN-ios-port.md) |
| 0068 | MADR | MADR 0068: Protocol v2 — reconnect-resilient transport | [0068-MADR-protocol-v2-reconnect-resilient-transport.md](decisions/0068-MADR-protocol-v2-reconnect-resilient-transport.md) |
| 0068 | PLAN | MADR 0068 — Implementation plan: protocol v2, reconnect-resilient transport | [0068-PLAN-protocol-v2-reconnect-resilient-transport.md](decisions/0068-PLAN-protocol-v2-reconnect-resilient-transport.md) |
| 0069 | MADR | MADR 0069: macOS permissions — sandbox parity, EPERM honesty, TCC identity | [0069-MADR-macos-permissions-and-sandbox-parity.md](decisions/0069-MADR-macos-permissions-and-sandbox-parity.md) |
| 0069 | PLAN | MADR 0069 — Implementation plan: macOS permissions | [0069-PLAN-macos-permissions-and-sandbox-parity.md](decisions/0069-PLAN-macos-permissions-and-sandbox-parity.md) |
| 0070 | MADR | MADR 0070: Deep-dive debugging pass — bugs, gaps, unfinished work | [0070-MADR-deep-dive-debugging-pass.md](decisions/0070-MADR-deep-dive-debugging-pass.md) |
| 0070 | PLAN | MADR 0070 — Implementation plan: deep-dive remediation | [0070-PLAN-deep-dive-remediation.md](decisions/0070-PLAN-deep-dive-remediation.md) |
| 0071 | MADR | MADR 0071: Codebase assessment — bugs, gaps, hardening, robustness, performance | [0071-MADR-codebase-assessment.md](decisions/0071-MADR-codebase-assessment.md) |
| 0071 | PLAN | MADR 0071 — Implementation plan: non-update remediation | [0071-PLAN-codebase-assessment-remediation.md](decisions/0071-PLAN-codebase-assessment-remediation.md) |
| 0072 | MADR | MADR 0072: Phone reconnect failure, hung agent sessions, host config audit | [0072-MADR-phone-reconnect-and-provider-timeout-incident.md](decisions/0072-MADR-phone-reconnect-and-provider-timeout-incident.md) |
| 0072 | PLAN | MADR 0072 — Implementation plan: reconnect, hang UX, host config, service heal | [0072-PLAN-phone-reconnect-and-provider-timeout-remediation.md](decisions/0072-PLAN-phone-reconnect-and-provider-timeout-remediation.md) |
| 0073 | MADR | MADR 0073: Goose prompt hang (silent 3600 s provider backoff) + codebase debug pass | [0073-MADR-goose-prompt-hang-and-debug-pass.md](decisions/0073-MADR-goose-prompt-hang-and-debug-pass.md) |
| 0074 | MADR | MADR 0074 — Remote Provider Authentication from Phone | [0074-MADR-remote-provider-auth-from-phone.md](decisions/0074-MADR-remote-provider-auth-from-phone.md) |
| 0074 | PLAN | Implement MADR 0074 — Remote provider auth from phone (W1 + W2 + W4 + W5) | [0074-PLAN-remote-provider-auth-from-phone.md](decisions/0074-PLAN-remote-provider-auth-from-phone.md) |
| 0075 | MADR | MADR 0075 — Kilo CLI as a session provider | [0075-MADR-kilo-cli-provider.md](decisions/0075-MADR-kilo-cli-provider.md) |
| 0075 | PLAN | Implement MADR 0075 — Kilo CLI as a session provider | [0075-PLAN-kilo-cli-provider.md](decisions/0075-PLAN-kilo-cli-provider.md) |
| 0076 | MADR | MADR 0076: Kilo CLI provider — debug pass (bugs, gaps, menu alignment) | [0076-MADR-kilo-debug-pass.md](decisions/0076-MADR-kilo-debug-pass.md) |
| 0076 | PLAN | Implement MADR 0076 — Kilo debug-pass remediation | [0076-PLAN-kilo-debug-pass-remediation.md](decisions/0076-PLAN-kilo-debug-pass-remediation.md) |
| 0077 | MADR | MADR 0077 — Signed receipts for permission decisions and session handoffs | [0077-MADR-signed-receipts-permission-handoffs.md](decisions/0077-MADR-signed-receipts-permission-handoffs.md) |
| 0077 | PLAN | Implement MADR 0077 — Signed receipts for permission decisions | [0077-PLAN-signed-receipts-permission-handoffs.md](decisions/0077-PLAN-signed-receipts-permission-handoffs.md) |
| 0078 | MADR | MADR 0078 — Device-to-device session handoff, with signed handoff receipts and phone-side receipt surfacing | [0078-MADR-session-handoff-and-receipt-surfacing.md](decisions/0078-MADR-session-handoff-and-receipt-surfacing.md) |
| 0078 | PLAN | Implement MADR 0078 — Session handoff + handoff receipts + phone receipt surfacing | [0078-PLAN-session-handoff-and-receipt-surfacing.md](decisions/0078-PLAN-session-handoff-and-receipt-surfacing.md) |
| 0079 | MADR | MADR 0079 — Prioritize configured model providers in a single drill-down model picker | [0079-MADR-provider-model-drill-down-picker.md](decisions/0079-MADR-provider-model-drill-down-picker.md) |
| 0079 | PLAN | Implement MADR 0079 — Provider/model drill-down picker | [0079-PLAN-provider-model-drill-down-picker.md](decisions/0079-PLAN-provider-model-drill-down-picker.md) |
| 0080 | MADR | MADR 0080: Add first-class Codex collaboration modes and app-server command parity | [0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md](decisions/0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md) |
| 0080 | PLAN | Implement MADR 0080 — First-class Codex collaboration modes and app-server command parity | [0080-PLAN-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md](decisions/0080-PLAN-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md) |
| 0081 | MADR | Adopt grok 1.0.3's measured CLI and ACP surface | [0081-MADR-grok-1.0.3-surface-parity.md](decisions/0081-MADR-grok-1.0.3-surface-parity.md) |
| 0081 | PLAN | Implement adopt grok 1.0.3's measured CLI and ACP surface | [0081-PLAN-grok-1.0.3-surface-parity.md](decisions/0081-PLAN-grok-1.0.3-surface-parity.md) |
| 0082 | MADR | MADR 0082 — Restructure settings into a hub with a graphically rich provider area | [0082-MADR-settings-provider-menu-ux-overhaul.md](decisions/0082-MADR-settings-provider-menu-ux-overhaul.md) |
| 0082 | PLAN | Implement restructure settings into a hub with a graphically rich provider area | [0082-PLAN-settings-provider-menu-ux-overhaul.md](decisions/0082-PLAN-settings-provider-menu-ux-overhaul.md) |
| 0083 | MADR | MADR 0083 — Make phone-driven provider auth actually complete: activation gaps and bottom-inset layout defects | [0083-MADR-provider-auth-activation-and-layout-gaps.md](decisions/0083-MADR-provider-auth-activation-and-layout-gaps.md) |
| 0083 | PLAN | Implement make phone-driven provider auth actually complete | [0083-PLAN-provider-auth-activation-and-layout-gaps.md](decisions/0083-PLAN-provider-auth-activation-and-layout-gaps.md) |
| 0084 | MADR | MADR 0084 — Harden the Android app: crash visibility, transcript-cache storage, and platform build gaps | [0084-MADR-android-app-hardening-and-performance.md](../apps/mobile/docs/decisions/0084-MADR-android-app-hardening-and-performance.md) |
| 0084 | PLAN | Implement Android app hardening: crash visibility, cache storage, and build gates | [0084-PLAN-android-app-hardening-and-performance.md](../apps/mobile/docs/decisions/0084-PLAN-android-app-hardening-and-performance.md) |
| 0085 | MADR | Select grok ACP auth methods from the live initialize catalog and write keys where grok actually reads them | [0085-MADR-grok-acp-auth-method-wiring.md](decisions/0085-MADR-grok-acp-auth-method-wiring.md) |
| 0085 | PLAN | Implement grok ACP auth-method wiring | [0085-PLAN-grok-acp-auth-method-wiring.md](decisions/0085-PLAN-grok-acp-auth-method-wiring.md) |
| 0086 | MADR | Treat a phone credential setup as complete only when the agent can actually use it | [0086-MADR-phone-provider-auth-completion.md](decisions/0086-MADR-phone-provider-auth-completion.md) |
| 0086 | PLAN | Implement treat a phone credential setup as complete only when the agent can actually use it | [0086-PLAN-phone-provider-auth-completion.md](decisions/0086-PLAN-phone-provider-auth-completion.md) |
| 0087 | MADR | Filter Kilo session chrome on the live wire and decode durable permission frames | [0087-MADR-kilo-session-chrome-and-permission-decode.md](decisions/0087-MADR-kilo-session-chrome-and-permission-decode.md) |
| 0087 | PLAN | Plan: Filter Kilo session chrome and decode durable permission frames | [0087-PLAN-kilo-session-chrome-and-permission-decode.md](decisions/0087-PLAN-kilo-session-chrome-and-permission-decode.md) |
| 0088 | MADR | Pin Kilo known-good to 7.4.22 and close the session-loop gaps that release added | [0088-MADR-kilo-7.4.22-surface-parity.md](decisions/0088-MADR-kilo-7.4.22-surface-parity.md) |
| 0088 | PLAN | Plan: Pin Kilo known-good to 7.4.22 and close session-loop gaps | [0088-PLAN-kilo-7.4.22-surface-parity.md](decisions/0088-PLAN-kilo-7.4.22-surface-parity.md) |
| 0089 | MADR | Keep long-running agent work alive across screen lock, transport blips, and host pressure | [0089-MADR-long-running-session-stability.md](decisions/0089-MADR-long-running-session-stability.md) |
| 0089 | PLAN | Plan: Long-running session stability (Doze, failover, prewarm, cgroups) | [0089-PLAN-long-running-session-stability.md](decisions/0089-PLAN-long-running-session-stability.md) |
| 0090 | MADR | Keep the install-time config.yaml in lockstep with the daemon config surface | [0090-MADR-config-template-completeness.md](decisions/0090-MADR-config-template-completeness.md) |
| 0090 | PLAN | Plan: Keep the install-time config.yaml in lockstep with the daemon config surface | [0090-PLAN-config-template-completeness.md](decisions/0090-PLAN-config-template-completeness.md) |
| 0091 | MADR | Harden the mcrelay daemon and its systemd unit without widening the trust model | [0091-MADR-mcrelay-daemon-hardening.md](decisions/0091-MADR-mcrelay-daemon-hardening.md) |
| 0091 | PLAN | Plan: Harden mcrelay's unit PATH, plaintext policy, and join-plane TLS surface | [0091-PLAN-mcrelay-daemon-hardening.md](decisions/0091-PLAN-mcrelay-daemon-hardening.md) |
| 0092 | MADR | Adopt grok 1.0.4's pinned session fork and re-pin the 0081 contract | [0092-MADR-grok-1.0.4-surface-parity.md](decisions/0092-MADR-grok-1.0.4-surface-parity.md) |
| 0092 | PLAN | Implement adopt grok 1.0.4's pinned session fork and re-pin the 0081 contract | [0092-PLAN-grok-1.0.4-surface-parity.md](decisions/0092-PLAN-grok-1.0.4-surface-parity.md) |
| 0093 | MADR | Resume a closed session without hanging, and list the sessions that actually survived | [0093-MADR-session-resume-hang-and-list-inversion.md](decisions/0093-MADR-session-resume-hang-and-list-inversion.md) |
| 0093 | PLAN | Implement session resume hang and list inversion | [0093-PLAN-session-resume-hang-and-list-inversion.md](decisions/0093-PLAN-session-resume-hang-and-list-inversion.md) |
| 0094 | MADR | Ending the only session from inside chat must return to a populated sessions list, not a black screen | [0094-MADR-end-session-return-black-screen.md](decisions/0094-MADR-end-session-return-black-screen.md) |
| 0094 | PLAN | Implement "Ending the only session from inside chat must return to a populated sessions list" | [0094-PLAN-end-session-return-black-screen.md](decisions/0094-PLAN-end-session-return-black-screen.md) |
| 0095 | MADR | Close the residual end-session defects 0094 left reachable, and fix the request-timeout and idempotency gaps behind them | [0095-MADR-post-0094-assessment-and-debug-pass.md](decisions/0095-MADR-post-0094-assessment-and-debug-pass.md) |
| 0095 | PLAN | Implement "Close the residual end-session defects 0094 left reachable, and fix the request-timeout and idempotency gaps behind them" | [0095-PLAN-post-0094-assessment-and-debug-pass.md](decisions/0095-PLAN-post-0094-assessment-and-debug-pass.md) |
| 0096 | MADR | Scope the model catalog to the session's own model provider, and order it deterministically | [0096-MADR-kilo-model-catalog-scope-and-order.md](decisions/0096-MADR-kilo-model-catalog-scope-and-order.md) |
| 0096 | PLAN | Implement scoping the model catalog to the session's model provider, and ordering it deterministically | [0096-PLAN-kilo-model-catalog-scope-and-order.md](decisions/0096-PLAN-kilo-model-catalog-scope-and-order.md) |
| 0097 | MADR | Ship a Linux-only `curl \| sh` bootstrap installer, and add `linux/arm64` to the release matrix | [0097-MADR-linux-curl-installer.md](decisions/0097-MADR-linux-curl-installer.md) |
| 0097 | PLAN | Implement the Linux `curl \| sh` bootstrap installer and `linux/arm64` releases | [0097-PLAN-linux-curl-installer.md](decisions/0097-PLAN-linux-curl-installer.md) |
| 0098 | MADR | Close the outstanding `install.sh` acceptance rows on ephemeral AWS and DigitalOcean hosts | [0098-MADR-ephemeral-cloud-install-verification.md](decisions/0098-MADR-ephemeral-cloud-install-verification.md) |
| 0098 | PLAN | Plan: Close the outstanding `install.sh` acceptance rows on ephemeral cloud hosts | [0098-PLAN-ephemeral-cloud-install-verification.md](decisions/0098-PLAN-ephemeral-cloud-install-verification.md) |
| 0098 | REPORT | Findings — sweep 0098, Session A (Alpine amd64, 2026-08-18) | [0098-REPORT-install-verification-sweep.md](../docs/reports/0098-REPORT-install-verification-sweep.md) |
| 0099 | MADR | Verify service state before reporting it, and repair the s6 and relay backends | [0099-MADR-installer-service-state-verification.md](decisions/0099-MADR-installer-service-state-verification.md) |
| 0099 | PLAN | Plan: Verify service state before reporting it, and repair the s6 and relay backends | [0099-PLAN-installer-service-state-verification.md](decisions/0099-PLAN-installer-service-state-verification.md) |
| 0099 | REPORT | 0099 re-verification results (v0.13.5 / v0.13.6) | [0099-REPORT-reverification.md](../docs/reports/0099-REPORT-reverification.md) |
| 0100 | MADR | Reconcile the service definition during `update`, and reload the manager before the restart | [0100-MADR-update-unit-refresh-and-daemon-reload.md](decisions/0100-MADR-update-unit-refresh-and-daemon-reload.md) |
| 0100 | PLAN | Plan: Reconcile the service definition during `update`, and reload the manager before the restart | [0100-PLAN-update-unit-refresh-and-daemon-reload.md](decisions/0100-PLAN-update-unit-refresh-and-daemon-reload.md) |
| 0100 | REPORT | 0100 Phase 0 — host confirmation of the update-refresh findings | [0100-REPORT-update-refresh.md](../docs/reports/0100-REPORT-update-refresh.md) |
| 0101 | MADR | Make Android agent alerts survive their own lifecycle, and make their absence diagnosable | [0101-MADR-android-agent-alert-delivery.md](../apps/mobile/docs/decisions/0101-MADR-android-agent-alert-delivery.md) |
| 0101 | PLAN | Plan: Make Android agent alerts survive their own lifecycle, and make their absence diagnosable | [0101-PLAN-android-agent-alert-delivery.md](../apps/mobile/docs/decisions/0101-PLAN-android-agent-alert-delivery.md) |
| 0102 | MADR | Add a `display_name` config parameter so the phone shows a friendly host name instead of an IP address | [0102-MADR-host-display-name-config.md](decisions/0102-MADR-host-display-name-config.md) |
| 0102 | PLAN | Implement host display name configuration | [0102-PLAN-host-display-name-config.md](decisions/0102-PLAN-host-display-name-config.md) |
| 0103 | MADR | Compare `update` against the published `BASE.N` release, and recycle a product's service only when that product has a unit file | [0103-MADR-update-tracks-release-build-and-active-service.md](decisions/0103-MADR-update-tracks-release-build-and-active-service.md) |
| 0103 | PLAN | Implement update tracking of published `BASE.N` and per-product service recycle | [0103-PLAN-update-tracks-release-build-and-active-service.md](decisions/0103-PLAN-update-tracks-release-build-and-active-service.md) |
| 0104 | MADR | Make the curl bootstrap installer first-class on Linux and macOS | [0104-MADR-installer-linux-and-macos.md](decisions/0104-MADR-installer-linux-and-macos.md) |
| 0104 | PLAN | Implement Linux + macOS curl bootstrap installer | [0104-PLAN-installer-linux-and-macos.md](decisions/0104-PLAN-installer-linux-and-macos.md) |
| 0105 | MADR | Require an approved MADR and PLAN before any mutating work | [0105-MADR-mutating-work-requires-madr-and-plan.md](decisions/0105-MADR-mutating-work-requires-madr-and-plan.md) |
| 0105 | PLAN | Implement mutating-work MADR/PLAN gate | [0105-PLAN-mutating-work-requires-madr-and-plan.md](decisions/0105-PLAN-mutating-work-requires-madr-and-plan.md) |
| 0106 | MADR | Adopt grok 1.0.5's ACP `_meta` model and effort surface, and stop treating spawn flags as applied | [0106-MADR-grok-1.0.5-surface-parity.md](decisions/0106-MADR-grok-1.0.5-surface-parity.md) |
| 0106 | PLAN | Implement grok 1.0.5 ACP `_meta` model and effort surface | [0106-PLAN-grok-1.0.5-surface-parity.md](decisions/0106-PLAN-grok-1.0.5-surface-parity.md) |
| 0107 | MADR | Phone card drives grok API-key and device-code auth; the host must not open a browser | [0107-MADR-grok-phone-api-key-and-device-code-auth.md](decisions/0107-MADR-grok-phone-api-key-and-device-code-auth.md) |
| 0107 | PLAN | Implement phone-card grok device-code auth (host must not open a browser) | [0107-PLAN-grok-phone-api-key-and-device-code-auth.md](decisions/0107-PLAN-grok-phone-api-key-and-device-code-auth.md) |
| 0108 | MADR | Pin Kilo known-good to 7.4.23 after wire and behavior compatibility verification | [0108-MADR-kilo-7.4.23-surface-parity.md](decisions/0108-MADR-kilo-7.4.23-surface-parity.md) |
| 0108 | PLAN | Plan: Verify and pin Kilo known-good to 7.4.23 | [0108-PLAN-kilo-7.4.23-surface-parity.md](decisions/0108-PLAN-kilo-7.4.23-surface-parity.md) |
| 0109 | MADR | Expand the Codex provider through capability-led app-server parity | [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](decisions/0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md) |
| 0109 | PLAN | Plan: Implement capability-led Codex app-server parity | [0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md](decisions/0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md) |
| 0110 | MADR | Stop Goose keychain prompts from blocking headless launch | [0110-MADR-goose-keyring-prompts-block-headless-launch.md](decisions/0110-MADR-goose-keyring-prompts-block-headless-launch.md) |
| 0110 | PLAN | Implement a Goose keyring-backend setting in mcremote configuration | [0110-PLAN-goose-keyring-prompts-block-headless-launch.md](decisions/0110-PLAN-goose-keyring-prompts-block-headless-launch.md) |
| 0111 | MADR | Stabilize asynchronous receipt test teardown | [0111-MADR-stabilize-asynchronous-receipt-test-teardown.md](decisions/0111-MADR-stabilize-asynchronous-receipt-test-teardown.md) |
| 0111 | PLAN | Implement stabilization of asynchronous receipt test teardown | [0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md](decisions/0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md) |
| 0112 | MADR | Pin OpenCode known-good to 1.18.21 after surface and behavior verification | [0112-MADR-opencode-1.18.21-surface-parity.md](decisions/0112-MADR-opencode-1.18.21-surface-parity.md) |
| 0112 | PLAN | Implement OpenCode 1.18.21 stable surface parity | [0112-PLAN-opencode-1.18.21-surface-parity.md](decisions/0112-PLAN-opencode-1.18.21-surface-parity.md) |
| 0113 | MADR | Close pre-existing unit-coverage debt as its own tracked work | [0113-MADR-preexisting-unit-coverage-debt.md](decisions/0113-MADR-preexisting-unit-coverage-debt.md) |
| 0113 | PLAN | Close pre-existing unit-coverage debt | [0113-PLAN-preexisting-unit-coverage-debt.md](decisions/0113-PLAN-preexisting-unit-coverage-debt.md) |
| 0114 | MADR | Manage markdownlint-cli2 with mise in the user environment | [0114-MADR-manage-markdownlint-cli2-with-mise.md](decisions/0114-MADR-manage-markdownlint-cli2-with-mise.md) |
| 0114 | PLAN | Plan: Install and pin markdownlint-cli2 with mise | [0114-PLAN-manage-markdownlint-cli2-with-mise.md](decisions/0114-PLAN-manage-markdownlint-cli2-with-mise.md) |
| 0115 | MADR | Remediate the mcrelay 2026-08 audit findings in one hardening pass | [0115-MADR-mcrelay-go126-audit-and-hardening.md](decisions/0115-MADR-mcrelay-go126-audit-and-hardening.md) |
| 0115 | PLAN | Implement the mcrelay 2026-08 hardening pass | [0115-PLAN-mcrelay-go126-audit-and-hardening.md](decisions/0115-PLAN-mcrelay-go126-audit-and-hardening.md) |
| 0116 | MADR | Ship mcremote and mcrelay on windows/amd64 and linux/arm64 behind a platform layer with native OS pathing | [0116-MADR-windows-and-linux-arm64-build-targets.md](decisions/0116-MADR-windows-and-linux-arm64-build-targets.md) |
| 0116 | PLAN | Implement the Windows and linux/arm64 build targets | [0116-PLAN-windows-and-linux-arm64-build-targets.md](decisions/0116-PLAN-windows-and-linux-arm64-build-targets.md) |
| 0117 | MADR | Diagnostic commands resolve paths without the serve-time readiness gate | [0117-MADR-diagnostic-commands-skip-serve-readiness-gate.md](decisions/0117-MADR-diagnostic-commands-skip-serve-readiness-gate.md) |
| 0117 | PLAN | PLAN 0117 — Diagnostic commands skip the serve-time readiness gate | [0117-PLAN-diagnostic-commands-skip-serve-readiness-gate.md](decisions/0117-PLAN-diagnostic-commands-skip-serve-readiness-gate.md) |
| 0118 | MADR | Symlink-dependent tests probe for the privilege rather than assuming the platform | [0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md](decisions/0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md) |
| 0118 | PLAN | PLAN 0118 — Symlink-dependent tests probe for the privilege | [0118-PLAN-symlink-dependent-tests-on-unprivileged-windows.md](decisions/0118-PLAN-symlink-dependent-tests-on-unprivileged-windows.md) |
| 0119 | MADR | Establish causation before repairing the codex tests that fail on linux/arm64 | [0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md](decisions/0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md) |
| 0119 | PLAN | PLAN 0119 — Establish causation, then repair the codex arm64 failures | [0119-PLAN-codex-tests-fail-on-the-linux-arm64-lane.md](decisions/0119-PLAN-codex-tests-fail-on-the-linux-arm64-lane.md) |
| 0120 | MADR | Retire darwin/amd64 and publish exactly four targets | [0120-MADR-retire-the-darwin-amd64-target.md](decisions/0120-MADR-retire-the-darwin-amd64-target.md) |
| 0120 | PLAN | PLAN 0120 — Retire darwin/amd64 and publish exactly four targets | [0120-PLAN-retire-the-darwin-amd64-target.md](decisions/0120-PLAN-retire-the-darwin-amd64-target.md) |
| 0121 | MADR | Achieve iPhone functional parity | [0121-MADR-achieve-iphone-functional-parity.md](decisions/0121-MADR-achieve-iphone-functional-parity.md) |
| 0121 | PLAN | PLAN 0121 — Achieve iPhone functional parity | [0121-PLAN-achieve-iphone-functional-parity.md](decisions/0121-PLAN-achieve-iphone-functional-parity.md) |
| 0122 | MADR | Make goose file-log tail attach observable so the Windows quota test is deterministic | [0122-MADR-deterministic-goose-file-log-tail-attach.md](decisions/0122-MADR-deterministic-goose-file-log-tail-attach.md) |
| 0122 | PLAN | PLAN 0122 — Deterministic goose file-log tail attach | [0122-PLAN-deterministic-goose-file-log-tail-attach.md](decisions/0122-PLAN-deterministic-goose-file-log-tail-attach.md) |
| 0123 | MADR | Move the chat session controls below the composer and give them one card idiom | [0123-MADR-unify-session-controls-below-the-composer.md](decisions/0123-MADR-unify-session-controls-below-the-composer.md) |
| 0123 | PLAN | PLAN 0123 — Session controls below the composer, one card idiom | [0123-PLAN-unify-session-controls-below-the-composer.md](decisions/0123-PLAN-unify-session-controls-below-the-composer.md) |
| 0124 | MADR | Parse entry filenames with a separator-agnostic basename, not `split('/')` | [0124-MADR-transcript-cache-path-separator.md](decisions/0124-MADR-transcript-cache-path-separator.md) |
| 0124 | PLAN | PLAN 0124 — Separator-agnostic entry names in the transcript cache | [0124-PLAN-transcript-cache-path-separator.md](decisions/0124-PLAN-transcript-cache-path-separator.md) |
| 0125 | MADR | Wait for launchd teardown instead of guessing at it, and restart on rollback | [0125-MADR-launchd-bootout-is-asynchronous.md](decisions/0125-MADR-launchd-bootout-is-asynchronous.md) |
| 0125 | PLAN | PLAN 0125 — Wait for launchd teardown; never leave the service stopped | [0125-PLAN-launchd-bootout-is-asynchronous.md](decisions/0125-PLAN-launchd-bootout-is-asynchronous.md) |
| 0126 | MADR | Fix the Android keep-alive and teardown defects found by the debugging pass, as one remediation pair | [0126-MADR-android-client-debugging-pass-findings.md](../apps/mobile/docs/decisions/0126-MADR-android-client-debugging-pass-findings.md) |
| 0126 | PLAN | PLAN 0126 — Make the Android client survive being backgrounded, and stop shipping surface nobody reviewed | [0126-PLAN-android-client-debugging-pass-findings.md](../apps/mobile/docs/decisions/0126-PLAN-android-client-debugging-pass-findings.md) |
| 0127 | MADR | Adopt Flutter 3.47.2 and move the local and CI pins together, instead of resolving the lockfile downward | [0127-MADR-adopt-current-flutter-toolchain.md](../apps/mobile/docs/decisions/0127-MADR-adopt-current-flutter-toolchain.md) |
| 0127 | PLAN | PLAN 0127 — Move to Flutter 3.47.2, pin and host together, and stop dependabot recreating the drift | [0127-PLAN-adopt-current-flutter-toolchain.md](../apps/mobile/docs/decisions/0127-PLAN-adopt-current-flutter-toolchain.md) |
| 0128 | MADR | Take four of the ten deferred items now, and give the rest named triggers instead of a backlog | [0128-MADR-triage-the-0126-and-0127-deferred-items.md](decisions/0128-MADR-triage-the-0126-and-0127-deferred-items.md) |
| 0128 | PLAN | PLAN 0128 — Clear the deferred lists: four fixes, four triggers, two closures | [0128-PLAN-triage-the-0126-and-0127-deferred-items.md](decisions/0128-PLAN-triage-the-0126-and-0127-deferred-items.md) |
| 0129 | MADR | Move the host connection into the foreground service's own isolate, so alerts survive a swipe | [0129-MADR-background-alert-delivery-survives-task-removal.md](decisions/0129-MADR-background-alert-delivery-survives-task-removal.md) |
| 0129 | PLAN | PLAN 0129 — Stop lying first, then move the connection into the service isolate | [0129-PLAN-background-alert-delivery-survives-task-removal.md](decisions/0129-PLAN-background-alert-delivery-survives-task-removal.md) |
| 0130 | MADR | The client can sit `connected` with no socket, and the cause is not yet known | [0130-MADR-client-can-sit-connected-with-no-socket.md](decisions/0130-MADR-client-can-sit-connected-with-no-socket.md) |
| 0130 | PLAN | Investigate: the client can sit `connected` with no socket | [0130-PLAN-client-can-sit-connected-with-no-socket.md](decisions/0130-PLAN-client-can-sit-connected-with-no-socket.md) |
| 0131 | MADR | Replace golint with golangci-lint, and pay the 85 findings that exposes | [0131-MADR-replace-golint-with-golangci-lint.md](decisions/0131-MADR-replace-golint-with-golangci-lint.md) |
| 0131 | PLAN | Implement: replace golint with golangci-lint | [0131-PLAN-replace-golint-with-golangci-lint.md](decisions/0131-PLAN-replace-golint-with-golangci-lint.md) |
| 0132 | MADR | The phone verifies the downloaded APK by GitHub's per-asset digest, not by a `SHA256SUMS` entry | [0132-MADR-verify-the-apk-by-github-asset-digest.md](decisions/0132-MADR-verify-the-apk-by-github-asset-digest.md) |
| 0132 | PLAN | Implement: verify the downloaded APK by GitHub's per-asset digest | [0132-PLAN-verify-the-apk-by-github-asset-digest.md](decisions/0132-PLAN-verify-the-apk-by-github-asset-digest.md) |
| 0133 | MADR | Startup credential recovery treats an unprovable observation as a no-op, not a terminal state | [0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md](decisions/0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md) |
| 0133 | PLAN | Implement: startup credential recovery treats an unprovable observation as a no-op | [0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md](decisions/0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md) |
| 0134 | MADR | An external Codex credential store is a supported state, not an ambiguity for an operator to resolve | [0134-MADR-an-external-credential-store-is-not-an-ambiguity.md](decisions/0134-MADR-an-external-credential-store-is-not-an-ambiguity.md) |
| 0134 | PLAN | Implement: an external Codex credential store is a supported state, not an ambiguity | [0134-PLAN-an-external-credential-store-is-not-an-ambiguity.md](decisions/0134-PLAN-an-external-credential-store-is-not-an-ambiguity.md) |
| 0135 | MADR | A schema change bumps the manifest version, and a refused manifest self-heals instead of silently disarming the provider | [0135-MADR-manifest-refusal-must-be-versioned-and-recoverable.md](decisions/0135-MADR-manifest-refusal-must-be-versioned-and-recoverable.md) |
| 0136 | MADR | Codex credential reality is read from `codex doctor --json`, not inferred from an exit code | [0136-MADR-classify-codex-auth-from-the-doctor-report.md](decisions/0136-MADR-classify-codex-auth-from-the-doctor-report.md) |
| 0136 | PLAN | Implement: read Codex credential reality from `codex doctor --json` | [0136-PLAN-classify-codex-auth-from-the-doctor-report.md](decisions/0136-PLAN-classify-codex-auth-from-the-doctor-report.md) |
| 0137 | MADR | The first-token latency regression is prompt weight, not transport — instrument the turn and control what enters the prompt | [0137-MADR-prompt-to-first-token-latency-regression.md](decisions/0137-MADR-prompt-to-first-token-latency-regression.md) |
| 0137 | PLAN | Implement: restore first-token latency — delivery, instrumentation, pins, and per-provider optimisation | [0137-PLAN-prompt-to-first-token-latency-regression.md](decisions/0137-PLAN-prompt-to-first-token-latency-regression.md) |
| 0138 | MADR | The turn path loses content and the provider surfaces are a ragged edge — fix delivery correctness first, then close the surface gap per provider | [0138-MADR-overhaul-provider-surfaces-and-turn-path.md](decisions/0138-MADR-overhaul-provider-surfaces-and-turn-path.md) |
| 0138 | PLAN | Implement the turn-path content fix, the RAM-backed transcript, and the provider surface close-out | [0138-PLAN-overhaul-provider-surfaces-and-turn-path.md](decisions/0138-PLAN-overhaul-provider-surfaces-and-turn-path.md) |
| 0139 | MADR | An error must not return a zero value that looks like an answer — make `goose.Reconcile`'s failure state nameable | [0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md](decisions/0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md) |
| 0139 | PLAN | Implement: name `goose.Reconcile`'s failure state and assert it | [0139-PLAN-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md](decisions/0139-PLAN-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md) |
| 0140 | MADR | A test helper wrapped file descriptor 0 and its finalizer closed stdin, corrupting unrelated files across `internal/daemon` | [0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md](decisions/0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md) |
| 0140 | PLAN | Implement: stop `quietLog` closing stdin, and guard the descriptor | [0140-PLAN-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md](decisions/0140-PLAN-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md) |
| 0141 | MADR | Backward history paging is built, tested and unreachable, and the path that serves a transcript logs nothing when it succeeds | [0141-MADR-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md](decisions/0141-MADR-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md) |
| 0141 | PLAN | Implement: reach the backward pager, seal the page join, and make the history path observable | [0141-PLAN-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md](decisions/0141-PLAN-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md) |
| 0142 | MADR | Remediate the mcrelay 2026-09 public-edge audit in one hardening pass | [0142-MADR-mcrelay-2026-09-public-edge-audit.md](decisions/0142-MADR-mcrelay-2026-09-public-edge-audit.md) |
| 0142 | PLAN | Plan: Remediate the mcrelay 2026-09 public-edge audit in one hardening pass | [0142-PLAN-mcrelay-2026-09-public-edge-audit.md](decisions/0142-PLAN-mcrelay-2026-09-public-edge-audit.md) |
| 0143 | MADR | CI retries a failed job once and records every retry, instead of repairing flakes one at a time | [0143-MADR-ci-retries-once-and-records-every-retry.md](decisions/0143-MADR-ci-retries-once-and-records-every-retry.md) |
| 0143 | PLAN | Implement: CI retries a failed job once and records every retry | [0143-PLAN-ci-retries-once-and-records-every-retry.md](decisions/0143-PLAN-ci-retries-once-and-records-every-retry.md) |
| 0144 | MADR | Add unit tests for wirecap redaction and CredentialMeta ordering | [0144-MADR-unit-tests-for-wirecap-and-credential-meta-ordering.md](decisions/0144-MADR-unit-tests-for-wirecap-and-credential-meta-ordering.md) |
| 0144 | PLAN | Implement: unit tests for wirecap and CredentialMeta ordering | [0144-PLAN-unit-tests-for-wirecap-and-credential-meta-ordering.md](decisions/0144-PLAN-unit-tests-for-wirecap-and-credential-meta-ordering.md) |
| 0145 | MADR | Local CI-style Windows tests catch go-native failures before GitHub | [0145-MADR-local-windows-ci-style-tests.md](decisions/0145-MADR-local-windows-ci-style-tests.md) |
| 0145 | PLAN | Implement: Local CI-style Windows tests | [0145-PLAN-local-windows-ci-style-tests.md](decisions/0145-PLAN-local-windows-ci-style-tests.md) |
| 0146 | MADR | Purge must tear down locally and detach from the caller's deadline | [0146-MADR-codex-purge-skips-local-teardown.md](decisions/0146-MADR-codex-purge-skips-local-teardown.md) |
| 0146 | PLAN | PLAN 0146 — Close before purge, and bound the engine call | [0146-PLAN-codex-purge-skips-local-teardown.md](decisions/0146-PLAN-codex-purge-skips-local-teardown.md) |
| 0147 | MADR | Make the Windows gate measure the code, not the shell, the PATH, or the checkout | [0147-MADR-windows-gate-measures-the-host-not-the-code.md](decisions/0147-MADR-windows-gate-measures-the-host-not-the-code.md) |
| 0147 | PLAN | PLAN 0147 — Make the Windows gate measure the code, not the shell, the PATH, or the checkout | [0147-PLAN-windows-gate-measures-the-host-not-the-code.md](decisions/0147-PLAN-windows-gate-measures-the-host-not-the-code.md) |
| 0148 | MADR | The CRLF is only in the working tree, so fixing it costs nothing to commit | [0148-MADR-the-crlf-is-only-in-the-working-tree.md](decisions/0148-MADR-the-crlf-is-only-in-the-working-tree.md) |
| 0148 | PLAN | PLAN 0148 — The CRLF is only in the working tree, so fixing it costs nothing to commit | [0148-PLAN-the-crlf-is-only-in-the-working-tree.md](decisions/0148-PLAN-the-crlf-is-only-in-the-working-tree.md) |
| 0149 | MADR | The derived switch scan can silently under-report, which is the drift it was written to prevent | [0149-MADR-the-derived-switch-scan-can-under-report.md](decisions/0149-MADR-the-derived-switch-scan-can-under-report.md) |
| 0149 | PLAN | PLAN 0149 — The derived switch scan can silently under-report | [0149-PLAN-the-derived-switch-scan-can-under-report.md](decisions/0149-PLAN-the-derived-switch-scan-can-under-report.md) |
| 0150 | MADR | The Windows tree-kill guarantee was designed, tested, documented, and never wired up | [0150-MADR-the-windows-tree-kill-was-never-wired-up.md](decisions/0150-MADR-the-windows-tree-kill-was-never-wired-up.md) |
| 0150 | PLAN | PLAN 0150 — Wire the Windows tree-kill guarantee | [0150-PLAN-the-windows-tree-kill-was-never-wired-up.md](decisions/0150-PLAN-the-windows-tree-kill-was-never-wired-up.md) |
| 0151 | MADR | wirecap redacts a home path, but the wire carries JSON — and more than a home path | [0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md](decisions/0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md) |
| 0151 | PLAN | PLAN 0151 — Make wirecap redaction match what the wire carries | [0151-PLAN-wirecap-redaction-does-not-know-what-the-wire-carries.md](decisions/0151-PLAN-wirecap-redaction-does-not-know-what-the-wire-carries.md) |
| 0152 | MADR | The skill is `writing-madr-and-plans`, and the second machine is why the repo kept saying otherwise | [0152-MADR-the-skill-name-is-writing-madr-and-plans.md](decisions/0152-MADR-the-skill-name-is-writing-madr-and-plans.md) |
| 0152 | PLAN | PLAN 0152 — Make every instruction file name the skill that exists | [0152-PLAN-the-skill-name-is-writing-madr-and-plans.md](decisions/0152-PLAN-the-skill-name-is-writing-madr-and-plans.md) |
| 0153 | MADR | Atomic writes fail on Windows whenever anything holds the destination open | [0153-MADR-atomic-writes-lose-a-race-any-windows-reader-can-start.md](decisions/0153-MADR-atomic-writes-lose-a-race-any-windows-reader-can-start.md) |
| 0153 | PLAN | PLAN 0153 — Survive a Windows reader holding the destination | [0153-PLAN-atomic-writes-lose-a-race-any-windows-reader-can-start.md](decisions/0153-PLAN-atomic-writes-lose-a-race-any-windows-reader-can-start.md) |
| 0154 | MADR | `mcrelay paths` refuses to print a path until the relay could serve | [0154-MADR-mcrelay-paths-demands-a-runnable-server.md](decisions/0154-MADR-mcrelay-paths-demands-a-runnable-server.md) |
| 0154 | PLAN | PLAN 0154 — Let `mcrelay paths` answer without a runnable relay | [0154-PLAN-mcrelay-paths-demands-a-runnable-server.md](decisions/0154-PLAN-mcrelay-paths-demands-a-runnable-server.md) |
| 0155 | MADR | The same shared secret is permission-guarded in mcrelay's config and unguarded in mcremote's | [0155-MADR-one-daemon-guards-its-config-the-other-does-not.md](decisions/0155-MADR-one-daemon-guards-its-config-the-other-does-not.md) |
| 0155 | PLAN | PLAN 0155 — Guard mcremote's config the way the rest of the product guards secrets | [0155-PLAN-one-daemon-guards-its-config-the-other-does-not.md](decisions/0155-PLAN-one-daemon-guards-its-config-the-other-does-not.md) |
| 0156 | MADR | Only the POSIX installer learned canonical asset names | [0156-MADR-only-the-posix-installer-learned-canonical-names.md](decisions/0156-MADR-only-the-posix-installer-learned-canonical-names.md) |
| 0156 | PLAN | PLAN 0156 — Teach the PowerShell installer both manifest shapes, and test it | [0156-PLAN-only-the-posix-installer-learned-canonical-names.md](decisions/0156-PLAN-only-the-posix-installer-learned-canonical-names.md) |
| 0157 | MADR | Daemon-owned rolling logs: each daemon writes, rotates, and bounds its own log file on every platform | [0157-MADR-daemon-owned-rolling-logs.md](decisions/0157-MADR-daemon-owned-rolling-logs.md) |
| 0157 | PLAN | PLAN 0157 — Daemon-owned rolling logs | [0157-PLAN-daemon-owned-rolling-logs.md](decisions/0157-PLAN-daemon-owned-rolling-logs.md) |
| 0158 | MADR | Move MADR/PLAN files to docs/spec/ subdirectory | [0158-MADR-move-madr-plan-files-to-docs-spec.md](decisions/0158-MADR-move-madr-plan-files-to-docs-spec.md) |
| 0158 | PLAN | PLAN 0158 — Move MADR/PLAN files to docs/spec/ subdirectory | [0158-PLAN-move-madr-plan-files-to-docs-spec.md](decisions/0158-PLAN-move-madr-plan-files-to-docs-spec.md) |
| 0159 | MADR | The Windows service path is half-wired: it installs a daemon it cannot refresh, cannot log, and mis-describes | [0159-MADR-the-windows-service-path-is-half-wired.md](decisions/0159-MADR-the-windows-service-path-is-half-wired.md) |
| 0159 | PLAN | PLAN 0159 — The Windows service path is half-wired: it installs a daemon it cannot refresh, cannot log, and mis-describes | [0159-PLAN-the-windows-service-path-is-half-wired.md](decisions/0159-PLAN-the-windows-service-path-is-half-wired.md) |
| 0160 | MADR | Remove Goose CLI support from the product, including its ACP-over-HTTP transport | [0160-MADR-remove-goose-cli-support.md](decisions/0160-MADR-remove-goose-cli-support.md) |
| 0160 | PLAN | PLAN 0160 — Remove Goose CLI support from the product, including its ACP-over-HTTP transport | [0160-PLAN-remove-goose-cli-support.md](decisions/0160-PLAN-remove-goose-cli-support.md) |
| 0161 | MADR | The mobile docs state the CI Flutter pin, not a stale 3.44 floor | [0161-MADR-mobile-docs-state-the-ci-flutter-pin.md](../apps/mobile/docs/decisions/0161-MADR-mobile-docs-state-the-ci-flutter-pin.md) |
| 0161 | PLAN | PLAN 0161 — The mobile docs state the CI Flutter pin, not a stale 3.44 floor | [0161-PLAN-mobile-docs-state-the-ci-flutter-pin.md](../apps/mobile/docs/decisions/0161-PLAN-mobile-docs-state-the-ci-flutter-pin.md) |
| 0162 | MADR | Config tests read the host's live config; give them an isolated root on every platform | [0162-MADR-config-tests-read-the-live-config.md](decisions/0162-MADR-config-tests-read-the-live-config.md) |
| 0162 | PLAN | PLAN 0162 — Config tests read the host's live config; give them an isolated root on every platform | [0162-PLAN-config-tests-read-the-live-config.md](decisions/0162-PLAN-config-tests-read-the-live-config.md) |
| 0163 | MADR | The Codex provider's jank is stale pins and a dead drift gate, not upstream churn | [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](decisions/0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md) |
| 0163 | PLAN | PLAN 0163 — The Codex provider's jank is stale pins and a dead drift gate | [0163-PLAN-codex-jank-is-stale-pins-not-upstream-churn.md](decisions/0163-PLAN-codex-jank-is-stale-pins-not-upstream-churn.md) |
| 0164 | MADR | Phone chat: large markdown tables need a scroll container, not column wrap | [0164-MADR-phone-markdown-table-mobile-rendering.md](../apps/mobile/docs/decisions/0164-MADR-phone-markdown-table-mobile-rendering.md) |
| 0164 | PLAN | PLAN 0164 — Phone chat: large markdown tables need a scroll container, not column wrap | [0164-PLAN-phone-markdown-table-mobile-rendering.md](../apps/mobile/docs/decisions/0164-PLAN-phone-markdown-table-mobile-rendering.md) |
| 0165 | MADR | Two CI flakes: a file lock that starves its waiter, and an assertion that outlived its design | [0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md](decisions/0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md) |
| 0165 | PLAN | PLAN 0165 — Queue the file lock, and stop a test asserting past its own fault point | [0165-PLAN-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md](decisions/0165-PLAN-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md) |
| 0166 | MADR | One stalled session can still take the ACP transport down | [0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md](decisions/0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md) |
| 0166 | PLAN | PLAN 0166 — Assert the containment we have, not the transport we cannot keep | [0166-PLAN-a-stalled-session-can-still-take-the-acp-transport-down.md](decisions/0166-PLAN-a-stalled-session-can-still-take-the-acp-transport-down.md) |
| 0167 | MADR | MADR 0167: `acp-go-sdk` tears the transport down on a full notification queue — contribute an overflow policy upstream | [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](decisions/0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md) |
| 0167 | PLAN | PLAN 0167 — Contribute a notification overflow policy to `acp-go-sdk` | [0167-PLAN-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](decisions/0167-PLAN-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md) |
| 0168 | MADR | MADR 0168: Build Android with JDK 21 in CI and on every dev host, keeping Java 17 bytecode | [0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](../apps/mobile/docs/decisions/0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md) |
| 0168 | PLAN | PLAN 0168 — Build Android with JDK 21 in CI and on every dev host | [0168-PLAN-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](../apps/mobile/docs/decisions/0168-PLAN-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md) |
| 0169 | MADR | MADR 0169: Standardize every toolchain on its newest supported, advisory-free stable release | [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](decisions/0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md) |
| 0169 | PLAN | PLAN 0169 — Standardize every toolchain on its newest supported, advisory-free stable release | [0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md](decisions/0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md) |
| 0170 | MADR | Make `make preflight` true: hermetic install tests, a degraded-manager fix, pinned staticcheck, and CI parity | [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](decisions/0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) |
| 0170 | PLAN | PLAN 0170 — Make `make preflight` true: hermetic install tests, a degraded-manager fix, pinned staticcheck, and CI parity | [0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](decisions/0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) |
| 0171 | MADR | Redact the identifiers already in the tree, so pushes pass the disclosure guard | [0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md](decisions/0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md) |
| 0171 | PLAN | PLAN 0171 — Redact the identifiers already in the tree, so pushes pass the disclosure guard | [0171-PLAN-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md](decisions/0171-PLAN-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md) |
| 0172 | MADR | Settings Reconnect now is centered under the Mesh/Relay control | [0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md](decisions/0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md) |
| 0172 | PLAN | PLAN 0172 — Settings Reconnect now is centered under the Mesh/Relay control | [0172-PLAN-settings-reconnect-now-centers-under-mesh-relay.md](decisions/0172-PLAN-settings-reconnect-now-centers-under-mesh-relay.md) |
| 0173 | MADR | Two CI flakes, two real defects: the relay closes a replaced host under the hub lock, and mobile tests leak transcript saves into the next test | [0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md](decisions/0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md) |
| 0173 | PLAN | PLAN 0173 — Fix two CI flakes: the hub-lock close and leaked transcript saves | [0173-PLAN-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md](decisions/0173-PLAN-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md) |
| 0174 | MADR | MADR 0174: Keep dependencies free of known advisories, with one gated exception | [0174-MADR-keep-dependencies-free-of-known-advisories.md](decisions/0174-MADR-keep-dependencies-free-of-known-advisories.md) |
| 0174 | PLAN | PLAN 0174 — Keep dependencies free of known advisories, with one gated exception | [0174-PLAN-keep-dependencies-free-of-known-advisories.md](decisions/0174-PLAN-keep-dependencies-free-of-known-advisories.md) |
| 0175 | MADR | Conform the docs tree to the adopted record layout | [0175-MADR-conform-docs-tree-to-adopted-record-layout.md](decisions/0175-MADR-conform-docs-tree-to-adopted-record-layout.md) |
| 0175 | PLAN | Implement conforming the docs tree to the adopted record layout | [0175-PLAN-conform-docs-tree-to-adopted-record-layout.md](decisions/0175-PLAN-conform-docs-tree-to-adopted-record-layout.md) |
| 0176 | MADR | MADR 0004: Phase 2 — Grok ACP provider | [0176-MADR-phase-2-grok-acp-provider.md](decisions/0176-MADR-phase-2-grok-acp-provider.md) |
| 0177 | REPORT | Phase 3 Plan: Flutter Android Client Assessment & Scaffolding | [0177-REPORT-flutter-android-client-assessment.md](../apps/mobile/docs/reports/0177-REPORT-flutter-android-client-assessment.md) |
| 0178 | REPORT | Magic CLI Remote — Mobile UX/UI Assessment & Research | [0178-REPORT-mobile-ux-assessment.md](../apps/mobile/docs/reports/0178-REPORT-mobile-ux-assessment.md) |
| 0179 | MADR | MADR 0179: Drive pigo as a first-class ACP provider — Spec-declared session operations, strict command fallback, and complete ACP event mapping | [0179-MADR-pigo-native-acp-provider.md](decisions/0179-MADR-pigo-native-acp-provider.md) |
| 0180 | MADR | Close 0175 leftovers and make docs gates fail the merge | [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](decisions/0180-MADR-check-records-is-a-preflight-and-ci-gate.md) |
| 0180 | PLAN | Implement closing 0175 leftovers and making docs gates fail the merge | [0180-PLAN-check-records-is-a-preflight-and-ci-gate.md](decisions/0180-PLAN-check-records-is-a-preflight-and-ci-gate.md) |
| 0181 | MADR | Make mcremote message size configurable | [0181-MADR-configurable-mcremote-message-size.md](decisions/0181-MADR-configurable-mcremote-message-size.md) |
| 0181 | PLAN | Implement configurable mcremote message size | [0181-PLAN-configurable-mcremote-message-size.md](decisions/0181-PLAN-configurable-mcremote-message-size.md) |
| 0182 | MADR | Match wrapped protocol event-type lines without retagging | [0182-MADR-protocol-event-line-continuation.md](decisions/0182-MADR-protocol-event-line-continuation.md) |
| 0182 | PLAN | PLAN 0182 — Match wrapped protocol event-type lines | [0182-PLAN-protocol-event-line-continuation.md](decisions/0182-PLAN-protocol-event-line-continuation.md) |
<!-- check_records.py ToC end -->

## I want to…

| I want to… | Start here |
| :--- | :--- |
| understand how this fits together | [architecture.md](architecture.md) |
| pair a phone | [Device pairing](../README.md#device-pairing) |
| run the daemon as a service | [Linux install](guides/ops-linux-install.md), [Windows install](guides/ops-windows-install.md) |
| configure mcrelay | [config-mcrelay.md](guides/config-mcrelay.md) |
| configure signed receipts | [receipts.md](guides/receipts.md) |
| set up Headscale | [headscale.md](guides/headscale.md) |
| sign Android or iOS builds | [Android signing](../apps/mobile/docs/guides/ops-android-signing.md), [iOS signing](../apps/mobile/docs/guides/ops-ios-signing.md) |
| understand the wire protocol | [protocol-v2.md](guides/protocol-v2.md) (delta over [protocol-v1.md](guides/protocol-v1.md)) |
| find why a decision was made | the records table above, starting at [0001-MADR-architecture-mcremote.md](decisions/0001-MADR-architecture-mcremote.md) |
