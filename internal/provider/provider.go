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

const (
	IDFake ID = "fake"
	IDGrok ID = "grok"
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

// PermissionSession can resolve remote permission prompts.
type PermissionSession interface {
	Session
	// RespondPermission selects an option for a pending permission_request.
	// If cancelled is true, the permission is rejected as cancelled.
	RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error
}

// Provider starts sessions for a given agent backend.
type Provider interface {
	ID() ID
	// Ready reports whether Start is expected to succeed (binary present, etc.).
	Ready() bool
	Start(ctx context.Context, opts StartOptions) (Session, error)
}
