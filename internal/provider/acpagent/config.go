package acpagent

import "time"

// Config configures an ACP CLI agent provider.
//
// Prewarm keeps one spare agent process spawned and ACP-initialized so the
// next session create/resume/relaunch skips the engine cold start (for
// opencode that is a full Bun runtime boot, ~3s measured). Only used by specs
// whose default argv is model-independent.
type Config struct {
	// Bin is the agent executable (default comes from the Spec).
	Bin string
	// Args are appended after the binary. Default comes from Spec.DefaultArgs.
	Args []string
	// AlwaysApprove auto-selects the first "allow"-style permission option
	// without remoting to the client. Prefer remote permissions for phones.
	AlwaysApprove bool
	// DefaultCWD is used when StartOptions.CWD is empty.
	DefaultCWD string
	// Model is the default model passed to the agent when non-empty (how it is
	// applied is Spec-specific, e.g. a CLI flag).
	Model string
	// PermissionTimeout bounds how long a remote permission request waits for a
	// client decision before the agent stops waiting and the action is treated
	// as cancelled. Zero disables the timeout (wait indefinitely). Prevents a
	// missed notification from hanging the agent forever.
	PermissionTimeout time.Duration
	// Prewarm keeps one spare initialized agent process ready (see package
	// doc above). The daemon must call Provider.EnsureWarm to arm it.
	Prewarm bool
	// TurnStallNotice emits a notice event when a running turn has produced no
	// agent output for this long — the "is it stuck?" signal that lets a user
	// decide to stop/reset instead of staring at a frozen spinner. Zero
	// disables it.
	TurnStallNotice time.Duration
}
