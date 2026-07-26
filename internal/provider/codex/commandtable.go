package codex

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
	"help":     {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	"clear":    {Kind: command.KindDaemon},
	"model":    {Kind: command.KindDaemon},
	"mode":     {Kind: command.KindDaemon},
	"context":  {Kind: command.KindDaemon},
	"compact":  {Kind: command.KindDaemon},
	"diff":     {Kind: command.KindNone, Note: "not supported"},
	"undo":     {Kind: command.KindNone, Note: "not supported"},
	"redo":     {Kind: command.KindNone, Note: "not supported"},
	"plan":     {Kind: command.KindNone, Note: "plan mode - probe required"},
	"goal":     {Kind: command.KindNone, Note: "goal mode - probe required"},
}
