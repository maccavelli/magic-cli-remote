package kilo

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Polling bounds for an engine-hosted device flow. The engine completes the
// token exchange itself for "auto"-mode flows, so all this loop does is watch
// for the credential to appear.
const (
	devicePollInterval = 5 * time.Second
	devicePollMax      = 15 * time.Minute
)

// StartDeviceAuth implements [httpagent.DeviceAuthDialect] (MADR 0074
// Strategy A).
//
// Kilo is the easiest of the five: the engine runs the whole flow. POST
// authorize returns a URL and instructions, the engine polls the vendor
// itself, and the credential lands in its own store. mcremote never sees a
// token — it displays a code and watches for the result.
//
// No oauth/callback call is made. The live probe (MADR 0074 §7.1) showed
// kilo's device flows complete on their own; posting a callback would be
// answering a question nobody asked.
func (d *httpDialect) StartDeviceAuth(
	ctx context.Context,
	api httpagent.API,
	upstreamID, methodID string,
	inputs map[string]string,
	_ bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return provider.DeviceFlow{}, nil, fmt.Errorf("kilo device auth: upstream id required")
	}
	methodIndex, err := methodIndexOf(upstreamID, methodID)
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}

	body := map[string]any{"method": methodIndex}
	if len(inputs) > 0 {
		body["inputs"] = inputs
	}
	var resp struct {
		URL          string `json:"url"`
		Method       string `json:"method"`
		Instructions string `json:"instructions"`
	}
	startCtx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()
	if err := api(startCtx, "POST",
		"/provider/"+url.PathEscape(upstreamID)+"/oauth/authorize", body, &resp); err != nil {
		return provider.DeviceFlow{}, nil, fmt.Errorf("kilo authorize %s: %w", upstreamID, err)
	}

	// MADR 0074 D7. resp.Method is deliberately ignored: the live probe found
	// it is "auto" for device *and* browser flows alike, so classification
	// comes from the URL shape.
	cls := providerauth.Classify(resp.URL, resp.Instructions)
	switch cls.Kind {
	case providerauth.FlowBrowser:
		return provider.DeviceFlow{}, nil, fmt.Errorf(
			"%s signs in through a browser on the host, which the phone cannot reach yet: %w",
			upstreamID, provider.ErrAuthUnsupported)
	case providerauth.FlowDevice:
	default:
		return provider.DeviceFlow{}, nil, fmt.Errorf(
			"kilo authorize %s returned no usable code", upstreamID)
	}

	d.log.Info("kilo device flow started",
		slog.String("upstream", upstreamID), slog.Int("method", methodIndex))

	flow := provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		ExpiresIn:       int(devicePollMax.Seconds()),
		Interval:        int(devicePollInterval.Seconds()),
	}
	wait := func(waitCtx context.Context) error {
		return d.awaitCredential(waitCtx, api, upstreamID)
	}
	return flow, wait, nil
}

// awaitCredential polls until the upstream reports a credential, the context
// ends, or the poll budget runs out.
func (d *httpDialect) awaitCredential(ctx context.Context, api httpagent.API, upstreamID string) error {
	ticker := time.NewTicker(devicePollInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(devicePollMax)

	for {
		select {
		case <-ctx.Done():
			// Cancelled or expired by the flow registry.
			return ctx.Err()
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("device sign-in for %s timed out", upstreamID)
		}
		configured, live := d.fetchConfiguredUpstreams(ctx, api)
		if !live {
			// The engine died mid-flow. Keep waiting rather than failing: the
			// transport respawns it, and the vendor-side flow is unaffected.
			continue
		}
		if _, ok := configured[upstreamID]; ok {
			d.log.Info("kilo device flow completed", slog.String("upstream", upstreamID))
			return nil
		}
	}
}

// methodIndexOf resolves a method id back to the array index kilo's authorize
// endpoint expects. Ids are minted as "<upstream>:<index>" by the catalog, so
// this is the inverse of that.
func methodIndexOf(upstreamID, methodID string) (int, error) {
	methodID = strings.TrimSpace(methodID)
	if methodID == "" {
		return 0, nil
	}
	prefix := upstreamID + ":"
	if !strings.HasPrefix(methodID, prefix) {
		return 0, fmt.Errorf("method %q does not belong to upstream %q", methodID, upstreamID)
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(methodID, prefix))
	if err != nil || idx < 0 {
		return 0, fmt.Errorf("method %q has no valid index", methodID)
	}
	return idx, nil
}
