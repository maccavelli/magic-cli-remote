package opencode

import (
	"context"
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// StartDeviceAuth implements [httpagent.DeviceAuthDialect] (MADR 0083 D3).
//
// Until this existed, every opencode method classified oauth_device failed
// with "unsupported for this provider" the moment the user tapped Start
// sign-in (MADR 0083 A2) — and the popular vendors are exactly the ones with
// oauth methods. The engine API is identical to kilo's (its OpenAPI declares
// the same POST /provider/{id}/oauth/authorize), so this is a shim over the
// shared flow with opencode's configured-set probe.
func (d *httpDialect) StartDeviceAuth(
	ctx context.Context,
	api httpagent.API,
	upstreamID, methodID string,
	inputs map[string]string,
	_ bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	return httpagent.StartEngineDeviceFlow(ctx, api, d.log, "opencode",
		upstreamID, methodID, inputs, d.configuredUpstreamSet)
}

// configuredUpstreamSet adapts connectedProviders to the shared poll hook:
// the id set of upstreams holding a credential, and whether the engine
// answered at all.
func (d *httpDialect) configuredUpstreamSet(ctx context.Context, api httpagent.API) (map[string]struct{}, bool) {
	conn, err := d.connectedProviders(ctx, api)
	if err != nil {
		d.log.Debug("opencode connected providers unavailable", slog.String("err", err.Error()))
		return nil, false
	}
	set := make(map[string]struct{}, len(conn.Providers))
	for _, p := range conn.Providers {
		set[p.ID] = struct{}{}
	}
	return set, true
}
