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
		Name:        "permissions",
		Args:        "[id]",
		Description: "Show or switch approval and sandbox permissions",
		Default:     Mapping{Kind: KindNone, Note: ReasonPermissionsNotMode},
	},
	{
		Name:        "reviewer",
		Args:        "[user|auto_review]",
		Description: "Show or switch who reviews approval requests",
		Default:     Mapping{Kind: KindNone, Note: "this agent exposes no separate approval reviewer"},
	},
	{
		Name:        "approve",
		Description: "Retry the exact most recent Guardian-denied action once",
		Default:     Mapping{Kind: KindNone, Note: "this agent has no tracked Guardian denial"},
	},
	{
		Name:        "model",
		Args:        "[name]",
		Description: "Show or switch the agent model",
		Default:     Mapping{Kind: KindDaemon},
	},
	{
		Name:        "thinking",
		Args:        "[level]",
		Description: "Show or switch the reasoning/thinking effort",
		// KindOp so the command is advertised only when the live session
		// implements ThinkingSession — opencode/goose never claim it
		// (MADR 0052 D6 / A4).
		Default: Mapping{Kind: KindOp, Op: OpSetThinkingLevel},
	},
	{
		Name:        "context",
		Description: "Show context-window usage for this session",
		Default:     Mapping{Kind: KindOp, Op: OpContext},
	},
	{
		Name:        "status",
		Description: "Show provider runtime status",
		Default:     Mapping{Kind: KindNone, Note: "this agent exposes no runtime status"},
	},
	{
		Name:        "usage",
		Description: "Show account, rate-limit, and context usage",
		Default:     Mapping{Kind: KindNone, Note: "this agent exposes no account usage"},
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
		Name:        "loop",
		Args:        "[interval] <prompt>",
		Description: "Run a prompt on a recurring interval",
		Default:     Mapping{Kind: KindNative, Native: "loop"},
	},
	{
		Name:        "fast",
		Args:        "[on|off]",
		Description: "Toggle the model's Fast service tier",
		Default:     Mapping{Kind: KindNone, Note: ReasonNoFastTier},
	},
	{
		Name:        "personality",
		Args:        "[friendly|pragmatic|none]",
		Description: "Show or set the model personality",
		Default:     Mapping{Kind: KindNone, Note: ReasonNoPersonality},
	},
	{
		Name:        "review",
		Args:        "[uncommitted|base <branch>|commit <sha>|custom <text>]",
		Description: "Start an inline code review",
		Default:     Mapping{Kind: KindNone, Note: ReasonNoReview},
	},
	{
		Name:        "fork",
		Args:        "[turn-id]",
		Description: "Fork this conversation into a new session",
		Default:     Mapping{Kind: KindNone, Note: ReasonNoFork},
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
