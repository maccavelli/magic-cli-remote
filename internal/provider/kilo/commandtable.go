package kilo

import "github.com/maccavelli/magic-cli-remote/internal/command"

// CommandTable declares how the Kilo engine satisfies the canonical
// slash-command vocabulary (MADR 0023), forked from the OpenCode table —
// every backing route was schema-confirmed in the 7.4.20 spike (MADR 0075
// §2.4: fork, revert/unrevert, summarize, diff, message list).
func (d *httpDialect) CommandTable() command.Table {
	return command.Table{
		"help":     {Kind: command.KindDaemon},
		"plan":     {Kind: command.KindMode, ModeID: "plan"},
		"mode":     {Kind: command.KindMode},
		"clear":    {Kind: command.KindDaemon},
		"new":      {Kind: command.KindDaemon},
		"sessions": {Kind: command.KindDaemon},
		// POST /api/session/{id}/model switches in place (v2 alias route,
		// present in the kilo OpenAPI dump).
		"model": {Kind: command.KindOp, Op: command.OpSetModel},
		"thinking": {
			Kind: command.KindNone,
			Note: "Kilo has no per-session thinking level — the model decides",
		},
		// Token counts come from the engine's own assistant messages.
		"context": {Kind: command.KindOp, Op: command.OpContext},
		// POST /session/{id}/summarize.
		"compact": {Kind: command.KindOp, Op: command.OpCompact},
		"diff":    {Kind: command.KindOp, Op: command.OpDiff},
		"undo":    {Kind: command.KindOp, Op: command.OpUndo},
		"redo":    {Kind: command.KindOp, Op: command.OpRedo},
		"goal": {
			Kind: command.KindNone,
			Note: "Kilo has no goal loop — state the objective in a prompt instead",
		},
		"deep-research": {
			Kind: command.KindNone,
			Note: "deep-research is a Grok-specific capability",
		},
		"workflow": {
			Kind: command.KindNone,
			Note: "workflows are not exposed by Kilo",
		},
		"permissions": {Kind: command.KindNone, Note: command.ReasonPermissionsNotMode},
		"fast":        {Kind: command.KindNone, Note: command.ReasonNoFastTier},
		"personality": {Kind: command.KindNone, Note: command.ReasonNoPersonality},
		"review":      {Kind: command.KindNone, Note: command.ReasonNoReview},
		"fork":        {Kind: command.KindOp, Op: command.OpFork},
	}
}
