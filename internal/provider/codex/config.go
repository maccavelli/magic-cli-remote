package codex

import (
	"time"
)

const (
	defaultPermissionTimeout = 900 * time.Second
	defaultStreamCoalesce    = 80 * time.Millisecond
	maxPendingChunkBytes     = 8 << 10
	engineStartTimeout       = 60 * time.Second
	engineStopTimeout        = 5 * time.Second
)

// Config holds user-supplied options for the Codex provider.
type Config struct {
	Bin               string
	AlwaysApprove     bool
	DefaultCWD        string
	Model             string
	PermissionTimeout time.Duration
	Prewarm           bool
	TurnStallNotice   time.Duration
	StreamCoalesce    *time.Duration
	ApprovalPolicy    string
	SandboxMode       string
	// AllowFullAccess advertises the "full-access" session mode (no approval
	// prompts and no sandbox). Off by default (MADR 0044 D5).
	AllowFullAccess bool
	// SandboxBrokenPolicy controls create behaviour when the Linux sandbox
	// cannot create a user namespace (MADR 0048). Valid: warn (default),
	// require_full_access, refuse.
	SandboxBrokenPolicy string
}

func (c Config) streamCoalesceWindow() time.Duration {
	if c.StreamCoalesce == nil {
		return defaultStreamCoalesce
	}
	return *c.StreamCoalesce
}
