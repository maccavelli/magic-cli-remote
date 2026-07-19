// Package provider defines the agent provider adapter interfaces.
package provider

import (
	"context"
	"errors"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// ErrNotImplemented indicates a provider is registered but not ready (Phase 1 stubs).
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
}

// Content is a prompt content block.
type Content struct {
	Type string // "text" in Phase 1
	Text string
}

// Session is a running agent conversation.
type Session interface {
	ID() string
	ProviderID() ID
	Prompt(ctx context.Context, parts []Content) error
	Cancel(ctx context.Context) error
	Events() <-chan event.Event
	Close(ctx context.Context) error
}

// Provider starts sessions for a given agent backend.
type Provider interface {
	ID() ID
	// Ready reports whether Start is expected to succeed.
	Ready() bool
	Start(ctx context.Context, opts StartOptions) (Session, error)
}
