// Package provider defines the agent provider adapter interfaces.
package provider

import (
	"context"
	"errors"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// ErrNotImplemented indicates a provider is registered but not ready.
var ErrNotImplemented = errors.New("provider not implemented")

// ID identifies a provider implementation.
type ID string

// Well-known provider IDs registered by the daemon.
const (
	// IDFake is the deterministic test/smoke provider.
	IDFake ID = "fake"
	// IDGrok is the Grok Build ACP provider.
	IDGrok ID = "grok"
	// IDOpencode is the OpenCode provider (HTTP or ACP transport).
	IDOpencode ID = "opencode"
)

// StartOptions configure a new agent session.
type StartOptions struct {
	CWD   string
	Model string
	Name  string
	// AgentSessionID, when set, asks the provider to resume/load an existing agent session.
	AgentSessionID string
	// LocalSessionID is the mcremote session id (optional; provider may generate if empty).
	LocalSessionID string
}

// Content is a prompt content block.
type Content struct {
	Type string // "text" in current phases
	Text string
}

// Session is a running agent conversation.
type Session interface {
	ID() string
	ProviderID() ID
	// AgentSessionID returns the provider-native session id when available.
	AgentSessionID() string
	Prompt(ctx context.Context, parts []Content) error
	Cancel(ctx context.Context) error
	Events() <-chan event.Event
	Close(ctx context.Context) error
}

// CWDSession is optionally implemented by sessions that resolve a concrete
// working directory (defaults, home-dir fallback). The manager prefers this
// over the caller-supplied path when populating session metadata, so clients
// see where the agent actually runs rather than an empty field.
type CWDSession interface {
	Session
	CWD() string
}

// PermissionSession can resolve remote permission prompts.
type PermissionSession interface {
	Session
	// RespondPermission selects an option for a pending permission_request.
	// If cancelled is true, the permission is rejected as cancelled.
	RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error
}

// PurgeSession optionally owns durable provider-side state that should be
// removed when the daemon hard-deletes a session (session.delete). Soft close
// must not call Purge — resume relies on that state remaining.
type PurgeSession interface {
	Session
	Purge(ctx context.Context) error
}

// Provider starts sessions for a given agent backend.
type Provider interface {
	ID() ID
	// Ready reports whether Start is expected to succeed (binary present, etc.).
	Ready() bool
	Start(ctx context.Context, opts StartOptions) (Session, error)
}
