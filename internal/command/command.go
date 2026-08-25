// Package command defines the canonical slash-command vocabulary the remote
// offers on every agent CLI, and resolves each command to the mechanism a
// given session actually supports.
//
// The vocabulary ([Specs]) is provider-independent. Each provider declares a
// [Table] saying how it satisfies each command — by a daemon-side
// implementation, a session mode, a provider capability, forwarding a command
// the agent really executes, or not at all with a reason. [Resolve] combines a
// table with what a live session reports ([SessionState]) and picks the
// mechanism, so the daemon, the /help text and the list advertised to clients
// all read the same answer.
//
// A table entry always beats what the agent advertises. That is the point of
// the table: advertisement is not capability. Grok, for example, advertises
// /compact and /context over ACP and executes neither — sending them returns no
// events at all — because its command catalog is really its terminal UI's
// (MADR 0023).
package command

import (
	"slices"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Kind is the mechanism that makes a canonical command work.
type Kind string

const (
	// KindDaemon means the daemon implements the command itself and it works on
	// every provider (/help, /new, /sessions).
	KindDaemon Kind = "daemon"
	// KindMode means the command maps onto a session mode (ACP session modes).
	KindMode Kind = "mode"
	// KindOp means the daemon performs it through a provider capability (an
	// optional interface on the live session, e.g. compaction).
	KindOp Kind = "op"
	// KindNative means the command is forwarded to the agent, which executes a
	// command of its own ([Mapping.Native]) and reports the result itself.
	KindNative Kind = "native"
	// KindNone means the command cannot work in this session; [Mapping.Note]
	// says why, in words a user reads.
	KindNone Kind = "none"
	// KindCollaborationMode maps onto the independent Codex collaboration-mode
	// axis (MADR 0080 D5). Distinct from KindMode (autonomy / ACP session modes).
	KindCollaborationMode Kind = "collaboration_mode"
)

// Op identifies a provider capability a canonical command needs. Whether a
// session has one is reported in [SessionState.Ops]; the daemon derives that
// from optional interfaces on the live session.
type Op string

const (
	// OpCompact summarises the conversation to reclaim context.
	OpCompact Op = "compact"
	// OpContext reports context-window usage. Daemon-side from the session's
	// last usage report rather than a provider call, so a session that has
	// never reported usage does not offer it.
	OpContext Op = "context"
	// OpStatus reports provider runtime status.
	OpStatus Op = "status"
	// OpUsage reports provider usage and rate-limit state.
	OpUsage Op = "usage"
	// OpApprovalsReviewer switches the independent approval reviewer axis.
	OpApprovalsReviewer Op = "approvals_reviewer"
	// OpGuardianApprove retries one exact tracked Guardian denial.
	OpGuardianApprove Op = "guardian_approve"
	// OpSetModel switches the model without restarting the agent.
	OpSetModel Op = "set_model"
	// OpSetThinkingLevel switches the reasoning/thinking effort for the
	// session (codex next-turn; grok spawn-only). Absent for providers that
	// expose no thinking control (MADR 0052 D6).
	OpSetThinkingLevel Op = "set_thinking_level"
	// OpDiff summarises the file changes made in the session.
	OpDiff Op = "diff"
	// OpUndo reverts the last turn's changes.
	OpUndo Op = "undo"
	// OpRedo restores the last undone turn.
	OpRedo Op = "redo"
	// OpGoal is a typed Codex goal operation (wired in MADR 0080 P7).
	OpGoal Op = "goal"
	// OpServiceTier is Fast / service-tier (wired in P6).
	OpServiceTier Op = "service_tier"
	// OpPersonality is model-gated personality (wired in P6).
	OpPersonality Op = "personality"
	// OpReview is an inline review turn (wired in P8).
	OpReview Op = "review"
	// OpFork forks the provider-native conversation.
	OpFork Op = "fork"
	// OpArchive archives or unarchives the provider-native conversation.
	OpArchive Op = "archive"
	// OpDelete permanently deletes the provider-native conversation after an
	// exact descendant-aware confirmation.
	OpDelete Op = "delete"
	// OpPS lists daemon-owned and negotiated native terminals.
	OpPS Op = "ps"
	// OpStop terminates one exact terminal id or all known terminals.
	OpStop Op = "stop"
)

// Frozen unavailable reasons (MADR 0080 stable-reason table).
const (
	ReasonIntegrationNotWired = "integration not wired"
	ReasonNoFastTier          = "this agent has no Fast service tier"
	ReasonNoPersonality       = "this agent has no personality setting"
	ReasonNoReview            = "this agent has no inline review command"
	ReasonNoFork              = "this agent can't fork a session over the remote"
	ReasonPermissionsNotMode  = "this agent uses /mode for agent modes, not permission presets"
)

// Mapping is how one provider satisfies one canonical command.
type Mapping struct {
	Kind Kind
	// Op is the capability required by a KindOp mapping.
	Op Op
	// Native is the agent command a KindNative mapping forwards, without the
	// leading slash. It need not equal the canonical name: grok answers
	// /context with its own /session-info.
	Native string
	// ModeID is the mode a KindMode mapping switches to. Empty means the
	// command only needs the session to have modes at all (/mode).
	ModeID string
	// Note explains a KindNone mapping to the user ("grok compacts only in its
	// own terminal UI"). It is also shown when a mapping turns out to be
	// unavailable at runtime.
	Note string
}

// Spec is one canonical command: what clients show, and the mapping to use for
// a provider whose table says nothing about it.
type Spec struct {
	Name string
	// Args is the usage hint shown after the name, e.g. "[off]".
	Args        string
	Description string
	// Aliases are additional spellings that resolve to this command.
	Aliases []string
	// Default applies when a provider's table has no entry for this command.
	// For agent-owned commands it is KindNative with Native == Name: forward it
	// when the agent advertises that name, and report it unavailable otherwise.
	// A provider overrides that either way.
	Default Mapping
}

// Table is a provider's declaration of how it satisfies the canonical
// vocabulary, keyed by canonical command name.
type Table map[string]Mapping

// Tabler is implemented by providers that declare a canonical command table.
// A provider that does not gets [Spec.Default] for every command, which is
// safe but rarely complete — see the checklist in MADR 0023.
type Tabler interface {
	CommandTable() Table
}

// Caveater is implemented by providers with a session-wide caveat worth showing
// in /help — something true of the agent rather than of one command (grok: part
// of the catalog it advertises only renders in its own terminal UI).
type Caveater interface {
	CommandCaveat() string
}

// SessionState is what a live session reports about itself, as far as command
// resolution is concerned.
type SessionState struct {
	// AgentCommands are the slash-command names the agent last advertised,
	// without leading slashes.
	AgentCommands []string
	// Modes are the session's operating modes, if any.
	Modes []event.SessionMode
	// CollaborationModes is the independent Plan/Default catalog.
	CollaborationModes []event.CollaborationMode
	// CurrentCollaborationModeID is the live collaboration selection.
	CurrentCollaborationModeID string
	// Ops reports the capabilities the session has. A missing key is false.
	Ops map[Op]bool
}

// Resolution is the mechanism chosen for one command in one session.
type Resolution struct {
	Spec Spec
	// Mapping is the mechanism to use. Kind is KindNone when unavailable.
	Mapping   Mapping
	Available bool
}

// Reason is the user-facing explanation for an unavailable command, or "" when
// it is available.
func (r Resolution) Reason() string {
	if r.Available {
		return ""
	}
	if r.Mapping.Note != "" {
		return r.Mapping.Note
	}
	return "this agent doesn't offer it"
}

// Usage renders the command as shown in help and autocomplete ("/plan [off]").
func (r Resolution) Usage() string {
	if r.Spec.Args == "" {
		return "/" + r.Spec.Name
	}
	return "/" + r.Spec.Name + " " + r.Spec.Args
}

// Lookup finds the canonical spec for a command name or alias,
// case-insensitively.
func Lookup(name string) (Spec, bool) {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "/")))
	if name == "" {
		return Spec{}, false
	}
	for _, s := range Specs {
		if s.Name == name || slices.Contains(s.Aliases, name) {
			return s, true
		}
	}
	return Spec{}, false
}

// Resolve picks the mechanism for a canonical command in one session. ok is
// false when the name is not part of the canonical vocabulary — the caller
// should then treat it as the agent's own command.
//
// The provider's mapping is tried first. An explicit KindNone is final: it
// exists precisely to override a command the agent advertises but cannot
// execute. Any other mapping that is unavailable in this session (a mode that
// is gone, a capability the session lacks, a native command the agent stopped
// advertising) falls back to the spec's default, and failing that to KindNone.
// Where the table says nothing at all, an agent advertising that exact name owns
// the command.
func Resolve(name string, t Table, s SessionState) (Resolution, bool) {
	spec, ok := Lookup(name)
	if !ok {
		return Resolution{}, false
	}
	if m, declared := t[spec.Name]; declared {
		if m.Kind == KindNone {
			return Resolution{Spec: spec, Mapping: m}, true
		}
		if available(m, s) {
			return Resolution{Spec: spec, Mapping: m, Available: true}, true
		}
	} else if advertises(s.AgentCommands, spec.Name) {
		// Undeclared, and the agent advertises this exact name: that is the best
		// evidence available that the agent owns the command, so it wins over
		// the spec's preferred default. A provider that knows better says so in
		// its table — which is how grok's inert /compact stays unavailable.
		return Resolution{
			Spec:      spec,
			Mapping:   Mapping{Kind: KindNative, Native: spec.Name},
			Available: true,
		}, true
	}
	if available(spec.Default, s) {
		return Resolution{Spec: spec, Mapping: spec.Default, Available: true}, true
	}
	return Resolution{Spec: spec, Mapping: Mapping{Kind: KindNone, Note: unavailableNote(spec)}}, true
}

// ResolveAll resolves every canonical command for a session, in [Specs] order.
// Used for /help and for the list advertised to clients.
func ResolveAll(t Table, s SessionState) []Resolution {
	out := make([]Resolution, 0, len(Specs))
	for _, spec := range Specs {
		r, _ := Resolve(spec.Name, t, s)
		out = append(out, r)
	}
	return out
}

// available reports whether a mapping's requirements hold in this session.
func available(m Mapping, s SessionState) bool {
	switch m.Kind {
	case KindDaemon:
		return true
	case KindMode:
		if len(s.Modes) == 0 {
			return false
		}
		if m.ModeID == "" {
			return true
		}
		return hasMode(s.Modes, m.ModeID)
	case KindCollaborationMode:
		if len(s.CollaborationModes) == 0 {
			return false
		}
		if m.ModeID == "" {
			return true
		}
		return hasCollaborationMode(s.CollaborationModes, m.ModeID)
	case KindOp:
		return s.Ops[m.Op]
	case KindNative:
		return m.Native != "" && advertises(s.AgentCommands, m.Native)
	default:
		return false
	}
}

// unavailableNote explains a command no mechanism could satisfy. Mode-backed
// commands get a more specific line than the generic one, since "this agent has
// no modes" is the whole story for them.
func unavailableNote(spec Spec) string {
	if spec.Default.Kind == KindMode {
		if spec.Default.ModeID != "" {
			return "this agent has no " + spec.Default.ModeID + " mode"
		}
		return "this agent doesn't expose switchable modes"
	}
	return "this agent doesn't offer it"
}

func hasMode(modes []event.SessionMode, id string) bool {
	for _, m := range modes {
		if strings.EqualFold(m.ID, id) {
			return true
		}
	}
	return false
}

func hasCollaborationMode(modes []event.CollaborationMode, id string) bool {
	for _, m := range modes {
		if strings.EqualFold(m.ID, id) {
			return true
		}
	}
	return false
}

func advertises(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimPrefix(n, "/"), want) {
			return true
		}
	}
	return false
}
