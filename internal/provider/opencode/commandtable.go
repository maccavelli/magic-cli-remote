package opencode

import "github.com/maccavelli/magic-cli-remote/internal/command"

// CommandTable implements [httpagent.CommandTabler]: how OpenCode satisfies the
// canonical slash-command vocabulary (MADR 0023).
//
// OpenCode advertises no canonical command of its own — `GET /command` returns
// only project commands and skills — but its HTTP API covers most of the set
// directly, so nearly everything here is a capability the daemon drives rather
// than text forwarded to the agent.
func (d *httpDialect) CommandTable() command.Table {
	return command.Table{
		"help":     {Kind: command.KindDaemon},
		"plan":     {Kind: command.KindMode, ModeID: "plan"},
		"mode":     {Kind: command.KindMode},
		"clear":    {Kind: command.KindDaemon},
		"new":      {Kind: command.KindDaemon},
		"sessions": {Kind: command.KindDaemon},
		// POST /api/session/{id}/model switches in place, so unlike grok this
		// costs the session nothing.
		"model": {Kind: command.KindOp, Op: command.OpSetModel},
		// OpenCode exposes no per-session thinking/effort control over the
		// HTTP API — only a boolean thinking toggle on some models, with no
		// settable ladder (MADR 0052 D6).
		"thinking": {
			Kind: command.KindNone,
			Note: "OpenCode has no per-session thinking level — the model decides",
		},
		// Token counts come from the engine's own assistant messages.
		"context":  {Kind: command.KindOp, Op: command.OpContext},
		"status":   {Kind: command.KindNone, Note: "OpenCode exposes no bounded host runtime status"},
		"usage":    {Kind: command.KindNone, Note: "OpenCode exposes no account usage summary"},
		"reviewer": {Kind: command.KindNone, Note: "OpenCode exposes no separate approval reviewer"},
		"approve":  {Kind: command.KindNone, Note: "OpenCode has no Guardian denial"},
		// POST /session/{id}/summarize. The v2 route (/api/session/{id}/compact)
		// answered 503 "not available yet" on 1.18.5.
		"compact": {Kind: command.KindOp, Op: command.OpCompact},
		"diff":    {Kind: command.KindOp, Op: command.OpDiff},
		"undo":    {Kind: command.KindOp, Op: command.OpUndo},
		"redo":    {Kind: command.KindOp, Op: command.OpRedo},
		"goal": {
			Kind: command.KindNone,
			Note: "OpenCode has no goal loop — state the objective in a prompt instead",
		},
		"deep-research": {
			Kind: command.KindNone,
			Note: "deep-research is a Grok-specific capability",
		},
		"workflow": {
			Kind: command.KindNone,
			Note: "workflows are not exposed by OpenCode",
		},
		"loop":        {Kind: command.KindNone, Note: "loop is a Grok-specific capability"},
		"permissions": {Kind: command.KindNone, Note: command.ReasonPermissionsNotMode},
		"fast":        {Kind: command.KindNone, Note: command.ReasonNoFastTier},
		"personality": {Kind: command.KindNone, Note: command.ReasonNoPersonality},
		"review":      {Kind: command.KindNone, Note: command.ReasonNoReview},
		"fork":        {Kind: command.KindOp, Op: command.OpFork},
	}
}
