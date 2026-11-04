package kilo

import (
	"context"
	"encoding/json"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

var _ httpagent.RuntimeDialect = (*httpDialect)(nil)

// providerUsagePath is kilo's plan usage: one item per plan the engine knows
// about, each with per-resource windows. An account with no plan answers an
// empty list, which is the normal shape for a bring-your-own-key setup.
const providerUsagePath = "/kilocode/provider-usage"

// RuntimeStatus implements [httpagent.RuntimeDialect]: kilo's plan usage, as
// the engine sent it, for the phone to show as JSON (MADR 0208 D12; PLAN P5
// Deviation 5). The providerUsage type in quota.go reads the same answer to
// confirm a limit after a turn fails; this reports it before one does.
func (d *httpDialect) RuntimeStatus(ctx context.Context, api httpagent.API) (provider.Result, error) {
	var body json.RawMessage
	if err := api(ctx, "GET", providerUsagePath, nil, &body); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{Method: "GET " + providerUsagePath, Body: body}, nil
}
