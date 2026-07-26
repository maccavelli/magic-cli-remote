package goose

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
	"help":     {Kind: command.KindDaemon},
	"plan":     {Kind: command.KindNone, Note: "goose has permission modes, not plan/build modes"},
	"mode":     {Kind: command.KindDaemon},
	"model":    {Kind: command.KindDaemon},
	"clear":    {Kind: command.KindDaemon},
	"new":      {Kind: command.KindDaemon},
	"sessions": {Kind: command.KindDaemon},
	"context":  {Kind: command.KindNone, Note: "goose doesn't expose token breakdown over ACP"},
	"compact":  {Kind: command.KindNative, Native: "compact"},
	"goal":     {Kind: command.KindNative, Native: "goal"},
	"diff":     {Kind: command.KindNone, Note: "no diff RPC over ACP"},
	"undo":     {Kind: command.KindNone, Note: "undo is git-based, not exposed over ACP"},
	"redo":     {Kind: command.KindNone, Note: "same as undo"},
	"status":   {Kind: command.KindNative, Native: "status"},
	"grind":    {Kind: command.KindNative, Native: "grind"},
	"skills":   {Kind: command.KindNative, Native: "skills"},
	"doctor":   {Kind: command.KindNative, Native: "doctor"},
}
