package goose

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
	"help":     {Kind: command.KindDaemon},
	"plan":     {Kind: command.KindNone, Note: "goose has permission modes, not plan/build modes"},
	"mode":     {Kind: command.KindNone, Note: "goose mode switching isn't wired up yet — restart the session to change mode"},
	"model":    {Kind: command.KindDaemon},
	"clear":    {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	"context":  {Kind: command.KindNone, Note: "goose doesn't expose token breakdown over ACP"},
	// Goose's terminal slash commands have no verified ACP execution contract.
	// Do not forward them simply because the local terminal accepts them.
	"compact": {Kind: command.KindNone, Note: "Goose compaction is not exposed through ACP"},
	"goal":    {Kind: command.KindNone, Note: "Goose goals are not exposed through ACP"},
	"diff":    {Kind: command.KindNone, Note: "no diff RPC over ACP"},
	"undo":    {Kind: command.KindNone, Note: "undo is git-based, not exposed over ACP"},
	"redo":    {Kind: command.KindNone, Note: "same as undo"},
}
