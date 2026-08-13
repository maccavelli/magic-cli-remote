package httpagent

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Polling bounds for an engine-hosted device flow. The engine completes the
// token exchange itself for "auto"-mode flows, so the loop only watches for
// the credential to appear in the engine's configured set.
const (
	devicePollInterval = 5 * time.Second
	devicePollMax      = 15 * time.Minute
	deviceStartTimeout = 20 * time.Second
)

// StartEngineDeviceFlow runs the OpenCode-family engine's device sign-in
// (MADR 0074 Strategy A, extracted for MADR 0083 D3): POST authorize returns
// a URL and instructions, the engine polls the vendor itself, and the
// credential lands in its own store. mcremote never sees a token — it
// displays a code and watches for the result.
//
// Both engines expose the identical endpoint (kilo probed in 0074 §7.1,
// opencode's OpenAPI probed 2026-08-13), so kilo and opencode call this with
// only their configured-set probe differing.
//
// No oauth/callback call is made: the live probe showed these flows complete
// on their own; posting a callback would be answering a question nobody
// asked.
func StartEngineDeviceFlow(
	ctx context.Context,
	api API,
	log *slog.Logger,
	agent, upstreamID, methodID string,
	inputs map[string]string,
	configured func(ctx context.Context, api API) (map[string]struct{}, bool),
) (provider.DeviceFlow, func(context.Context) error, error) {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return provider.DeviceFlow{}, nil, fmt.Errorf("%s device auth: upstream id required", agent)
	}
	methodIndex, err := EngineMethodIndex(upstreamID, methodID)
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
	startCtx, cancel := context.WithTimeout(ctx, deviceStartTimeout)
	defer cancel()
	if err := api(startCtx, "POST",
		"/provider/"+url.PathEscape(upstreamID)+"/oauth/authorize", body, &resp); err != nil {
		return provider.DeviceFlow{}, nil, fmt.Errorf("%s authorize %s: %w", agent, upstreamID, err)
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
			"%s authorize %s returned no usable code", agent, upstreamID)
	}

	log.Info("device flow started",
		slog.String("agent", agent),
		slog.String("upstream", upstreamID), slog.Int("method", methodIndex))

	flow := provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		ExpiresIn:       int(devicePollMax.Seconds()),
		Interval:        int(devicePollInterval.Seconds()),
	}
	wait := func(waitCtx context.Context) error {
		return awaitEngineCredential(waitCtx, api, log, agent, upstreamID, configured)
	}
	return flow, wait, nil
}

// awaitEngineCredential polls until the upstream reports a credential, the
// context ends, or the poll budget runs out.
func awaitEngineCredential(
	ctx context.Context,
	api API,
	log *slog.Logger,
	agent, upstreamID string,
	configured func(ctx context.Context, api API) (map[string]struct{}, bool),
) error {
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
		set, live := configured(ctx, api)
		if !live {
			// The engine died mid-flow. Keep waiting rather than failing: the
			// transport respawns it, and the vendor-side flow is unaffected.
			continue
		}
		if _, ok := set[upstreamID]; ok {
			log.Info("device flow completed",
				slog.String("agent", agent), slog.String("upstream", upstreamID))
			return nil
		}
	}
}

// EngineMethodIndex resolves a method id back to the array index the engine's
// authorize endpoint expects. Ids are minted as "<upstream>:<index>" by the
// catalog, so this is the inverse of that.
func EngineMethodIndex(upstreamID, methodID string) (int, error) {
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
