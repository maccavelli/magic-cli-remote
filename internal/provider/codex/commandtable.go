package codex

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
	"help":     {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	"clear":    {Kind: command.KindDaemon},

	// thread/compact/start — verified OK in the 0.145.0 spike (MADR 0028 §16.3).
	"compact": {Kind: command.KindOp, Op: command.OpCompact},
	// codex's model is per-turn: SetModel updates opts.Model and beginTurn
	// sends it on the next turn/start (session.go:310-317), so the thread
	// survives. KindDaemon would relaunch the agent and lose the
	// conversation (commands.go:225,654-675).
	"model": {Kind: command.KindOp, Op: command.OpSetModel},
	// thread/tokenUsage/updated feeds lastUsage, which gates OpContext
	// (commands.go:53).
	"context": {Kind: command.KindOp, Op: command.OpContext},

	// Codex exposes Plan and Default collaboration modes, but the set
	// path is unverified (MADR 0028 §16.7). Until it is, say so in words
	// a user can act on — the note is displayed verbatim (command.go:143).
	"mode": {Kind: command.KindNone,
		Note: "codex mode switching isn't wired up yet — start a new session to change mode"},
	"plan": {Kind: command.KindNone,
		Note: "codex plan mode isn't wired up yet — ask the agent to plan before it edits"},
	"goal": {Kind: command.KindNone,
		Note: "codex goals aren't exposed over the app-server protocol"},
	"diff": {Kind: command.KindNone,
		Note: "codex exposes no diff over the app-server — ask the agent to show its changes"},
	"undo": {Kind: command.KindNone,
		Note: "codex can't undo a turn remotely — ask the agent to revert its changes"},
	"redo": {Kind: command.KindNone,
		Note: "codex can't redo a turn remotely"},
}
