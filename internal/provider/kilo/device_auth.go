package kilo

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// StartDeviceAuth implements [httpagent.DeviceAuthDialect] (MADR 0074
// Strategy A). The engine runs the whole flow; the shared implementation
// (httpagent.StartEngineDeviceFlow, extracted for MADR 0083 D3 when opencode
// gained the same wiring) authorizes, classifies the URL shape (D7), and
// watches this dialect's configured set for the credential to appear.
func (d *httpDialect) StartDeviceAuth(
	ctx context.Context,
	api httpagent.API,
	upstreamID, methodID string,
	inputs map[string]string,
	_ bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	return httpagent.StartEngineDeviceFlow(ctx, api, d.log, "kilo",
		upstreamID, methodID, inputs, d.fetchConfiguredUpstreams)
}
