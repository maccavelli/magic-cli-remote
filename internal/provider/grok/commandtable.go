package grok

import "github.com/maccavelli/magic-cli-remote/internal/command"

// commandTable is how grok satisfies the canonical slash-command vocabulary
// (MADR 0023). Every entry is what grok 0.2.112 was observed to do over ACP
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
	// ACP has no set-model call and grok exposes none as an extension, so a
	// model change means relaunching the agent (the daemon's own path).
	"model": {Kind: command.KindDaemon},
	// Grok's own /context renders in its TUI and returns nothing here;
	// /session-info reports the same numbers as a message.
	"context": {Kind: command.KindNative, Native: "session-info"},
	"goal":    {Kind: command.KindNative, Native: "goal"},
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
}

// commandCaveat covers the rest of grok's advertised catalog: commands beyond
// the canonical set are forwarded as-is, and the TUI-only ones among them
// (/always-approve, /feedback, …) answer with silence.
const commandCaveat = "Some of grok's own commands only work in its terminal UI " +
	"and return nothing here."
