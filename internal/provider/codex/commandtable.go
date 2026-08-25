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
	// turn/start.effort is per-turn, so /thinking takes effect on the next
	// message and keeps the thread (MADR 0052 §2.1).
	"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel},
	// thread/tokenUsage/updated feeds lastUsage, which gates OpContext
	// (commands.go:53).
	"context": {Kind: command.KindOp, Op: command.OpContext},
	"status":  {Kind: command.KindOp, Op: command.OpStatus},
	"usage":   {Kind: command.KindOp, Op: command.OpUsage},

	"mode":        {Kind: command.KindMode},
	"permissions": {Kind: command.KindMode},
	"reviewer":    {Kind: command.KindOp, Op: command.OpApprovalsReviewer},
	"approve":     {Kind: command.KindOp, Op: command.OpGuardianApprove},
	"plan":        {Kind: command.KindCollaborationMode, ModeID: "plan"},
	"goal":        {Kind: command.KindOp, Op: command.OpGoal},
	"deep-research": {Kind: command.KindNone,
		Note: "deep-research is a Grok-specific capability"},
	"workflow": {Kind: command.KindNone,
		Note: "codex workflows are not exposed over the app-server protocol"},
	"loop":        {Kind: command.KindNone, Note: "loop is a Grok-specific capability"},
	"diff":        {Kind: command.KindOp, Op: command.OpDiff},
	"fast":        {Kind: command.KindOp, Op: command.OpServiceTier},
	"personality": {Kind: command.KindOp, Op: command.OpPersonality},
	"review":      {Kind: command.KindOp, Op: command.OpReview},
	"fork":        {Kind: command.KindOp, Op: command.OpFork},
	"archive":     {Kind: command.KindOp, Op: command.OpArchive},
	"delete":      {Kind: command.KindOp, Op: command.OpDelete},
	"undo": {Kind: command.KindNone,
		Note: "codex can't undo a turn remotely — ask the agent to revert its changes"},
	"redo": {Kind: command.KindNone,
		Note: "codex can't redo a turn remotely"},
}
