// Package grok is a Phase 1 stub for the Grok Build ACP provider.
package grok

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Provider is the Grok Build adapter stub.
type Provider struct {
	Bin string
}

// New returns a Grok provider stub.
func New(bin string) *Provider {
	if bin == "" {
		bin = "grok"
	}
	return &Provider{Bin: bin}
}

func (p *Provider) ID() provider.ID { return provider.IDGrok }
func (p *Provider) Ready() bool     { return false }

func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	_ = ctx
	_ = opts
	return nil, fmt.Errorf("grok provider (bin=%s): not implemented in phase 1: %w", p.Bin, provider.ErrNotImplemented)
}
