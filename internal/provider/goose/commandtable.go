package goose

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
	"help": {Kind: command.KindDaemon},
	"plan": {Kind: command.KindNone, Note: "goose has permission modes, not plan/build modes"},
	"mode": {Kind: command.KindNone, Note: "goose mode switching isn't wired up yet — restart the session to change mode"},
	// Goose switches model in place via ACP session/set_config_option, so
	// /model must not relaunch the agent — KindDaemon cost the conversation for
	// a change the agent can make without restarting (MADR 0043 D6).
	"model":    {Kind: command.KindOp, Op: command.OpSetModel},
	"thinking": {Kind: command.KindNone, Note: "goose has no selectable thinking level over ACP"},
	"clear":    {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	"context":  {Kind: command.KindNone, Note: "goose doesn't expose token breakdown over ACP"},
	// Goose's terminal slash commands have no verified ACP execution contract.
	// Do not forward them simply because the local terminal accepts them.
	"compact":       {Kind: command.KindNone, Note: "Goose compaction is not exposed through ACP"},
	"goal":          {Kind: command.KindNone, Note: "Goose goals are not exposed through ACP"},
	"deep-research": {Kind: command.KindNone, Note: "deep-research is a Grok-specific capability"},
	"workflow":      {Kind: command.KindNone, Note: "workflows are not exposed over ACP by Goose"},
	"diff":          {Kind: command.KindNone, Note: "no diff RPC over ACP"},
	"undo":          {Kind: command.KindNone, Note: "undo is git-based, not exposed over ACP"},
	"redo":          {Kind: command.KindNone, Note: "same as undo"},
	"permissions":   {Kind: command.KindNone, Note: "goose mode switching isn't wired up yet — restart the session to change mode"},
	"fast":          {Kind: command.KindNone, Note: command.ReasonNoFastTier},
	"personality":   {Kind: command.KindNone, Note: command.ReasonNoPersonality},
	"review":        {Kind: command.KindNone, Note: command.ReasonNoReview},
	"fork":          {Kind: command.KindNone, Note: command.ReasonNoFork},
}
