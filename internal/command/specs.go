package command

// Specs is the canonical slash-command vocabulary, in the order clients show
// it. Every provider is expected to declare a [Mapping] for each of these (see
// the checklist in MADR 0023); a command left undeclared falls back to
// [Spec.Default].
//
// Defaults prefer a daemon-side mechanism over forwarding, because a command an
// agent advertises is not necessarily one it executes. Only /goal, which has no
// daemon-side equivalent, defaults to forwarding.
var Specs = []Spec{
	{
		Name:        "help",
		Description: "List the commands available in this session",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "plan",
		Args:        "[off]",
		Description: "Plan without editing; /plan off returns to the default mode",
		Default:     Mapping{Kind: KindMode, ModeID: "plan"},
	},
	{
		Name:        "mode",
		Args:        "[id]",
		Description: "Show or switch the agent's operating mode",
		Default:     Mapping{Kind: KindMode},
	},
	{
		Name:        "model",
		Args:        "[name]",
		Description: "Show or switch the agent model",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "context",
		Description: "Show context-window usage for this session",
		Default:     Mapping{Kind: KindOp, Op: OpContext},
	},
	{
		Name:        "compact",
		Description: "Summarise the conversation to reclaim context",
		Default:     Mapping{Kind: KindOp, Op: OpCompact},
	},
	{
		Name:        "clear",
		Aliases:     []string{"reset"},
		Description: "Clear the conversation and restart the agent",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "new",
		Args:        "[name]",
		Description: "Start a new agent session",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "sessions",
		Description: "List your sessions",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "goal",
		Args:        "<objective>",
		Description: "Set an autonomous goal for the agent to work toward",
		Default:     Mapping{Kind: KindNative, Native: "goal"},
	},
	{
		Name:        "deep-research",
		Args:        "<query>",
		Description: "Research with bounded parallel agents, cross-check evidence, and write a cited report",
		Default:     Mapping{Kind: KindNative, Native: "deep-research"},
	},
	{
		Name:        "workflow",
		Args:        "<name> [args] | pause|resume|stop|save [name]",
		Description: "Launch a saved workflow, or manage a run",
		Default:     Mapping{Kind: KindNative, Native: "workflow"},
	},
	{
		Name:        "diff",
		Description: "Show the file changes made in this session",
		Default:     Mapping{Kind: KindOp, Op: OpDiff},
	},
	{
		Name:        "undo",
		Description: "Undo the last turn's changes",
		Default:     Mapping{Kind: KindOp, Op: OpUndo},
	},
	{
		Name:        "redo",
		Description: "Restore the last undone turn",
		Default:     Mapping{Kind: KindOp, Op: OpRedo},
	},
}
