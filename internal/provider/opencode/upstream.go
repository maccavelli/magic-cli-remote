package opencode

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// SetActiveUpstream implements [httpagent.UpstreamSwitchDialect] (MADR 0074
// D14), mirroring the kilo dialect.
//
// "Active upstream" for an OpenCode-family engine means the provider half of
// the default model: the vendor a new session bills against when the caller
// names no model. Switching therefore means picking that vendor's own default
// model out of the engine's connected-provider catalog.
//
// This is the MADR 0073 escape — move off a quota-blocked vendor to one that is
// already authenticated, with no credential work — and OpenCode needs it as
// much as goose did: `opencode-go` is the same weekly-quota product that wedged
// goose.
func (d *httpDialect) SetActiveUpstream(ctx context.Context, api httpagent.API, upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()

	conn, err := d.connectedProviders(ctx, api)
	if err != nil {
		return fmt.Errorf("read connected providers: %w", err)
	}
	var found bool
	for _, p := range conn.Providers {
		if p.ID == upstreamID {
			found = true
			break
		}
	}
	if !found {
		// Refusing beats accepting: a switch to an unauthenticated vendor would
		// look like it worked and then fail every turn.
		return fmt.Errorf("no connected provider %q", upstreamID)
	}
	model := conn.Default[upstreamID]
	if model == "" {
		for _, p := range conn.Providers {
			if p.ID != upstreamID {
				continue
			}
			for id, m := range p.Models {
				if m.ID != "" {
					model = m.ID
				} else {
					model = id
				}
				break
			}
		}
	}
	if model == "" {
		return fmt.Errorf("provider %q advertises no models", upstreamID)
	}

	d.mu.Lock()
	d.defaultModelProvider, d.defaultModelID = upstreamID, model
	d.mu.Unlock()
	d.log.Info("opencode active upstream switched",
		slog.String("upstream", upstreamID), slog.String("model", model))
	return nil
}
