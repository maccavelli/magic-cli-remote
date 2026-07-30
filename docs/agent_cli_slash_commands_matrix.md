# Agent CLI Commands & ACP Standard Comparison Matrix

> [!WARNING]
> **Superseded in part — do not implement from the matrix below.** Probing grok
> 0.2.112, OpenCode 1.18.5 and goose 1.44.0 on 2026-07-25 contradicted it: no
> command is advertised by all of them (grok ∩ goose is `{compact, goal}`),
> none advertises `/help` or `/plan`, and — most importantly — grok *advertises*
> `/compact` and `/context` over ACP while executing neither. The corrected
> per-CLI reality, and the canonical vocabulary we ship instead, are in
> [MADR 0023](./0023-MADR-canonical-slash-commands.md). This file is kept as the
> original survey.

This document provides a comprehensive deep-dive into the agent tools and slash commands supported and advertised across four major agentic CLI platforms: **OpenCode**, **Goose**, **Grok CLI**, and **Codex CLI**, along with their integration into the **Agent Client Protocol (ACP)** standard and HTTP/REST APIs.

---

## 1. Overview of Target Platforms & Protocols

* **OpenCode CLI**: An open-source, terminal-native AI engineering platform featuring a TUI and a headless REST API (`opencode serve`). Exposes commands via `.opencode/commands/` and OpenAPI endpoints (`/session/:id/command`).
* **Goose CLI**: An open-source AI agent platform by Block built for developer workflows, supporting custom recipes, Model Context Protocol (MCP), and Agent Client Protocol (ACP).
* **Grok CLI (Grok Build)**: An autonomous coding agent platform by xAI operating in terminal TUIs, offering sandboxed microVM tool execution, subagent orchestration, and ACP compatibility.
* **Codex CLI**: OpenAI's terminal coding agent interface optimized for iterative planning, diff inspection, and autonomous task loops.
* **Agent Client Protocol (ACP)**: An open JSON-RPC 2.0 standard (analogous to LSP for agents) that decouples agent engines from IDEs and CLI terminals. Standardizes how agents advertise available slash commands (`available_commands_update`) and process user commands (`session/prompt`).

---

## 2. Platform Command & Feature Capability Matrix

| Slash Command / Tool | OpenCode | Goose CLI | Grok CLI | Codex CLI | ACP Advertised? | Function & Purpose |
| :--- | :---: | :---: | :---: | :---: | :---: | :--- |
| **`/help`** | ✅ | ✅ | ✅ | ✅ | ✅ | Displays interactive command menu, keybindings, and help documentation. |
| **`/plan`** | ✅ | ✅ | ✅ | ✅ | ✅ | Enters planning mode; generates a step-by-step implementation blueprint before editing code. |
| **`/goal`** | ✅ | ✅ | ✅ | ✅ | ✅ | Launches an autonomous execution loop (plan -> execute -> verify) for high-level objectives. |
| **`/compact`** | ✅ | ✅ | ✅ | ✅ | ✅ | Summarizes conversation history to compress context size and optimize token usage. |
| **`/context`** | ✅ | ✅ | ✅ | ✅ | ✅ | Displays or manages current context window usage, active file attachments, and prompt limits. |
| **`/clear`** | ✅ | ✅ | ✅ | ✅ | ✅ | Clears session history and state to begin a clean, unencumbered context. |
| **`/model`** | ✅ | ✅ | ✅ | ✅ | ✅ | Switches active LLM provider or model architecture during a live session. |
| **`/sessions`** | ✅ | ✅ | ✅ | ✅ | ✅ | Lists, loads, switches, or manages historical and active agent sessions. |
| **`/undo` / `/redo`** | ✅ | ⚠️ (via git) | ✅ | ✅ | Optional | Reverts or reapplies recent agent edits or session interaction turns. |
| **`/diff`** | ✅ | ⚠️ (via git) | ✅ | ✅ | Optional | Renders interactive diffs of file modifications made by the agent. |
| **`/auto` / `/permissions`**| ✅ | ✅ | ✅ | ✅ | Optional | Adjusts agent autonomy levels (auto-approving safe tool calls vs. human-in-the-loop). |

> [!NOTE]
> All 4 CLI platforms advertise slash commands dynamically to clients using ACP notifications (`available_commands_update`), enabling autocomplete popups in compliant editors (e.g., Zed, JetBrains, VS Code, and terminal TUIs).

---

## 3. Core Shared Set of Common Slash Commands

Across all four target CLI platforms and official ACP specifications, the following **8 core slash commands** represent the universal baseline supported by every platform:

### 1. `/help`
* **Purpose**: Command discovery and usage assistance.
* **ACP Standard Representation**: Queries advertised command list.

### 2. `/plan`
* **Purpose**: Enforces structured thinking by producing a step-by-step strategy or roadmap before executing filesystem changes.

### 3. `/goal`
* **Purpose**: Triggers autonomous, multi-turn task execution where the agent loops through planning, subagent execution, and verification until the task is complete.

### 4. `/compact`
* **Purpose**: Frees context window space by running an automated history summarization step without discarding key architectural decisions.

### 5. `/context`
* **Purpose**: Inspects active context metrics (token breakdown, attached files, system prompts) and manages prompt injection limits.

### 6. `/clear` (or `/reset`, `/new`)
* **Purpose**: Flushes active session conversation memory for a clean start.

### 7. `/model` (or `/models`)
* **Purpose**: Dynamically switches the active inference model or adjusts parameters (e.g. reasoning effort).

### 8. `/sessions`
* **Purpose**: Lists, loads, switches, or archives agent sessions.

---

## 4. API & Protocol Endpoints for Command Execution

### Agent Client Protocol (ACP - JSON-RPC 2.0)
* **Command Advertisement Notification**: `session/available_commands_update`
* **Command Prompt Execution**: `session/prompt` (payload contains `/command <arguments>`)

### OpenCode REST API (`opencode serve`)
* **Trigger Command**: `POST /session/:id/command`
  ```json
  {
    "command": "/plan",
    "arguments": "refactor authentication module"
  }
  ```
* **Prompt Endpoint**: `POST /session/:id/prompt`

---

## 5. Summary Recommendation

For developers or integrations targeting universal interoperability across OpenCode, Goose, Grok, and Codex CLI platforms, standardizing on the **8 shared core commands** (`/help`, `/plan`, `/goal`, `/compact`, `/context`, `/clear`, `/model`, `/sessions`) guarantees 100% compatibility across all current ACP-compliant agent tools and HTTP/RPC endpoints.
