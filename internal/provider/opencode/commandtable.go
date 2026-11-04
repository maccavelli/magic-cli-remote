package opencode

import "github.com/maccavelli/magic-cli-remote/internal/command"

// acpCommandTable is how OpenCode v2 over ACP satisfies the canonical
// vocabulary (MADR 0023; 0214 PLAN P4 step 5).
//
// opencode 2.2.3 advertises init, review, goal and compact as its own commands
// (G1's available_commands_update), and runs a slash command sent as prompt
// text (packages/cli/src/acp/prompt.ts:41-55), so those forward. The model,
// effort and mode are standard config options. The 1.x HTTP routes this
// dialect's table drove (summarize, diff, revert, fork) are not on the ACP
// surface.
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
		// Each model advertises its own reasoning rungs as the `variants`
		// record, and prompt/command accept the chosen key as `variant`, so the
		// level is settable per request and mid-session (MADR 0112 A14).
		"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel},
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
		"archive":     {Kind: command.KindNone, Note: "opencode exposes no native archive operation"},
		"delete":      {Kind: command.KindNone, Note: "opencode exposes no native permanent-delete operation"},
		"ps":          {Kind: command.KindNone, Note: "opencode exposes no terminal registry"},
		"stop":        {Kind: command.KindNone, Note: "opencode exposes no stoppable terminals"},
	}
}
