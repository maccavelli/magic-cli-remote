package grok

import "github.com/maccavelli/magic-cli-remote/internal/command"

// commandTable is how grok satisfies the canonical slash-command vocabulary
// (MADR 0023). Every entry is what grok 1.0.5 was observed to do over ACP
// stdio, not what it advertises: its available_commands list is really its TUI's
// catalog, and several entries return nothing at all over the protocol. Probed
// in the same session, `/session-info` answered with a full report while
// `/compact`, `/context` and `/always-approve` produced zero session/update
// frames and zero tokens.
var commandTable = command.Table{
	"help":     {Kind: command.KindDaemon},
	"plan":     {Kind: command.KindMode, ModeID: "plan"},
	"mode":     {Kind: command.KindMode},
	"clear":    {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	// model switches the live model mid-session via ACP session/set_model
	// (verified live against grok 0.2.112; MADR 0039 D1). grok validates
	// the id against its live model list and rejects unknown ids.
	"model": {Kind: command.KindOp, Op: command.OpSetModel},
	// 1.0.5 session/set_model `_meta.reasoningEffort` is mid-session
	// (MADR 0106). Spawn argv --reasoning-effort is an ACP no-op.
	"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel},
	// Grok's own /context renders in its TUI and returns nothing here;
	// /session-info reports the same numbers as a message.
	"context":       {Kind: command.KindNative, Native: "session-info"},
	"status":        {Kind: command.KindNone, Note: "grok exposes no host runtime status over ACP"},
	"usage":         {Kind: command.KindNone, Note: "grok exposes no account usage over ACP"},
	"goal":          {Kind: command.KindNative, Native: "goal"},
	"deep-research": {Kind: command.KindNative, Native: "deep-research"},
	"workflow":      {Kind: command.KindNative, Native: "workflow"},
	"loop":          {Kind: command.KindNative, Native: "loop"},
	"compact": {
		Kind: command.KindNone,
		Note: "grok compacts only in its own terminal UI — over the remote /compact returns nothing",
	},
	"diff": {
		Kind: command.KindNone,
		Note: "grok exposes no diff over ACP — ask the agent to show its changes",
	},
	"undo": {
		Kind: command.KindNone,
		Note: "grok can't undo a turn over ACP — ask the agent to revert its changes",
	},
	"redo": {
		Kind: command.KindNone,
		Note: "grok can't redo a turn over ACP",
	},
	"permissions": {Kind: command.KindNone, Note: command.ReasonPermissionsNotMode},
	"fast":        {Kind: command.KindNone, Note: command.ReasonNoFastTier},
	"personality": {Kind: command.KindNone, Note: command.ReasonNoPersonality},
	// 1.0.5 advertises the bundled review skill under this name; KindNone
	// hid it while /help also listed it under "From the agent". This is not
	// Codex OpReview. available() is false when the agent stops advertising
	// review (MADR 0081 P3.12).
	"review": {Kind: command.KindNative, Native: "review"},
	// 1.0.5 _x.ai/session/fork {sourceSessionId,sourceCwd,newCwd} returns
	// newSessionId; Manager.Fork then session/load on a new process
	// (MADR 0092 P1.1). available() is false if the live session is not
	// a ForkSession (should not happen on acpagent).
	"fork": {Kind: command.KindOp, Op: command.OpFork},
}

// commandCaveat covers the rest of grok's advertised catalog: commands beyond
// the canonical set are forwarded as-is, and the TUI-only ones among them
// (/always-approve, /feedback, …) answer with silence.
const commandCaveat = "Some of grok's own commands only work in its terminal UI " +
	"and return nothing here."
