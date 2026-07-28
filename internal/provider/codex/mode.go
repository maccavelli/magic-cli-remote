package codex

import (
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Codex has no mode list of its own: what the TUI presents as a mode is really
// a pair of engine settings, an approval policy and a sandbox. Both are carried
// per thread (thread/start) and per turn (turn/start), so a mode switch is a
// genuine runtime operation rather than something fixed at session create
// (MADR 0044 D5).
//
// Note the shape asymmetry between the two, which is easy to cross-wire:
// thread/start takes `sandbox` as a kebab-case SandboxMode *string*, while
// turn/start takes `sandboxPolicy` as an object with a camelCase type tag.
// applyPolicyParams and sandboxPolicyParam own one each.

const (
	modeReadOnly   = "read-only"
	modeAuto       = "auto"
	modeFullAccess = "full-access"
)

// codexMode is one advertised mode and the engine policy it maps to.
type codexMode struct {
	mode           event.SessionMode
	approvalPolicy string // AskForApproval: untrusted | on-request | never
	sandbox        string // SandboxMode:    read-only | workspace-write | danger-full-access
}

// codexModes is the full table. `auto` pairs `never` with `workspace-write`
// rather than `danger-full-access` on purpose: removing the human from the loop
// and removing the sandbox are separate decisions, and an unattended session
// should still be contained. Codex's own "Auto" preset is on-request +
// workspace-write, which still prompts at the workspace boundary — not what
// auto-approve means here.
var codexModes = []codexMode{
	{
		mode: event.SessionMode{
			ID: modeReadOnly, Name: modeReadOnly,
			Description: "Read files; ask before anything else",
		},
		approvalPolicy: "on-request",
		sandbox:        "read-only",
	},
	{
		mode: event.SessionMode{
			ID: modeAuto, Name: modeAuto,
			Description: "Auto-approve — no prompts; edits confined to the workspace",
			Dangerous:   true,
		},
		approvalPolicy: "never",
		sandbox:        "workspace-write",
	},
	{
		mode: event.SessionMode{
			ID: modeFullAccess, Name: "full access",
			Description: "Auto-approve with no sandbox (dangerous)",
			Dangerous:   true,
		},
		approvalPolicy: "never",
		sandbox:        "danger-full-access",
	},
}

// availableCodexModes is the table filtered by config: full-access is hidden
// unless explicitly allowed, so it cannot be reached by a stray tap.
func availableCodexModes(cfg Config) []codexMode {
	out := make([]codexMode, 0, len(codexModes))
	for _, m := range codexModes {
		if m.mode.ID == modeFullAccess && !cfg.AllowFullAccess {
			continue
		}
		out = append(out, m)
	}
	return out
}

// advertisedModes is the event-facing view of availableCodexModes.
func advertisedModes(cfg Config) []event.SessionMode {
	avail := availableCodexModes(cfg)
	out := make([]event.SessionMode, 0, len(avail))
	for _, m := range avail {
		out = append(out, m.mode)
	}
	return out
}

// findCodexMode resolves a mode id against the modes this config advertises.
func findCodexMode(cfg Config, id string) (codexMode, bool) {
	id = strings.TrimSpace(id)
	for _, m := range availableCodexModes(cfg) {
		if strings.EqualFold(m.mode.ID, id) {
			return m, true
		}
	}
	return codexMode{}, false
}

// modeIDFor reports which advertised mode a policy pair corresponds to, or ""
// when it matches none.
//
// Returning "" rather than guessing matters: the mode chip is the user's only
// signal about whether the agent can act unattended, so advertising a list with
// no current id is honest where naming the wrong one is not.
func modeIDFor(cfg Config, approvalPolicy, sandbox string) string {
	for _, m := range availableCodexModes(cfg) {
		if m.approvalPolicy == approvalPolicy && m.sandbox == sandbox {
			return m.mode.ID
		}
	}
	return ""
}

// maxApprovalSummary caps the audit line so one approval carrying a long
// command cannot flood the transcript.
const maxApprovalSummary = 120

// approvalSummary renders an auto-approved request as "shell (git status)" for
// the audit notice. Tool name and its arguments only — never prompt text.
func approvalSummary(toolName, detail string) string {
	if toolName == "" {
		toolName = "approval"
	}
	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\n", " "))
	out := toolName
	if detail != "" {
		out = toolName + " (" + detail + ")"
	}
	return truncateRunes(out, maxApprovalSummary)
}

// truncateRunes shortens s to at most maxRunes runes, appending an ellipsis
// when it cuts. It counts runes rather than bytes so a multi-byte character in
// a path or command is never split down the middle, and so the ellipsis itself
// (3 bytes) cannot push the result past the cap.
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(r[:maxRunes-1]) + "…"
}

// sandboxPolicyParam maps a SandboxMode string to turn/start's SandboxPolicy
// object — an internally tagged enum with camelCase variant names. Verified
// against codex 0.145: turn/start rejects the kebab-case string form that
// thread/start requires (MADR 0044 Finding 4).
func sandboxPolicyParam(mode string) map[string]any {
	switch mode {
	case "read-only":
		return map[string]any{"type": "readOnly", "networkAccess": false}
	case "workspace-write":
		return map[string]any{
			"type":          "workspaceWrite",
			"networkAccess": false,
			"writableRoots": []string{},
		}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}
