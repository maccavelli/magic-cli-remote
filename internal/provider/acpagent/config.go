package acpagent

import "time"

// Config configures an ACP CLI agent provider.
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
}
