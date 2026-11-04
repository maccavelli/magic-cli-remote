package opencode

import "github.com/maccavelli/magic-cli-remote/internal/command"

// acpCommandTable is how OpenCode v2 over ACP satisfies the canonical
// vocabulary (MADR 0023; 0214 PLAN P4 step 5).
//
// opencode 2.2.3 advertises init, review, goal and compact as its own commands
// (G1's available_commands_update), and runs a slash command sent as prompt
// text (packages/cli/src/acp/prompt.ts:41-55), so those forward. The model,
// effort and mode are standard config options. /diff, /undo and /redo are
// the control plane's (0214 PLAN P6d, P6e); fork is not on either surface.
func acpCommandTable() command.Table {
	return command.Table{
		"help":     {Kind: command.KindDaemon},
		"plan":     {Kind: command.KindMode, ModeID: "plan"},
		"mode":     {Kind: command.KindMode},
		"clear":    {Kind: command.KindDaemon},
		"new":      {Kind: command.KindDaemon},
		"sessions": {Kind: command.KindDaemon},
		// The model config option switches in place.
		"model": {Kind: command.KindOp, Op: command.OpSetModel},
		// The effort config option, offered only while the current model has
		// variants (acp/config-option.ts:36-48), so the session declares the
		// operation only then.
		"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel},
		// Token counts come from usage_update.
		"context": {Kind: command.KindOp, Op: command.OpContext},
		"compact": {Kind: command.KindNative, Native: "compact"},
		// New in 2.2.1.
		"goal":     {Kind: command.KindNative, Native: "goal"},
		"review":   {Kind: command.KindNative, Native: "review"},
		"status":   {Kind: command.KindNone, Note: "OpenCode exposes no bounded host runtime status"},
		"usage":    {Kind: command.KindNone, Note: "OpenCode exposes no account usage summary"},
		"reviewer": {Kind: command.KindNone, Note: "OpenCode exposes no separate approval reviewer"},
		"approve":  {Kind: command.KindNone, Note: "OpenCode has no Guardian denial"},
		// Served by the control plane (0214 PLAN P6d, P6e).
		"diff": {Kind: command.KindOp, Op: command.OpDiff},
		"undo": {Kind: command.KindOp, Op: command.OpUndo},
		"redo": {Kind: command.KindOp, Op: command.OpRedo},
		"fork": {
			Kind: command.KindNone,
			Note: "Magic does not fork OpenCode v2 sessions yet",
		},
		"deep-research": {Kind: command.KindNone, Note: "OpenCode has no deep-research command"},
		"workflow":      {Kind: command.KindNone, Note: "workflows are not exposed by OpenCode"},
		"loop":          {Kind: command.KindNone, Note: "OpenCode has no loop command"},
		"permissions":   {Kind: command.KindNone, Note: command.ReasonPermissionsNotMode},
		"fast":          {Kind: command.KindNone, Note: command.ReasonNoFastTier},
		"personality":   {Kind: command.KindNone, Note: command.ReasonNoPersonality},
		"archive":       {Kind: command.KindNone, Note: "opencode exposes no native archive operation"},
		"delete":        {Kind: command.KindNone, Note: "opencode exposes no native permanent-delete operation"},
		"ps":            {Kind: command.KindNone, Note: "opencode exposes no terminal registry"},
		"stop":          {Kind: command.KindNone, Note: "opencode exposes no stoppable terminals"},
	}
}
