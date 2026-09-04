package kilo

import (
	"context"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

var _ httpagent.RuntimeDialect = (*httpDialect)(nil)

// RuntimeStatus implements [httpagent.RuntimeDialect] over kilo's plan usage.
//
// It reuses the providerUsage type in quota.go, which MADR 0138 Phase 6.4
// transcribed from the engine's own OpenAPI document. That type exists to
// confirm a limit after a turn has failed; this reports the same data before
// one does, which is the question `/status` asks.
//
// kilo says more here than any other engine in this repository: its windows
// carry an explicit per-resource state, so "not throttled" is an answer it can
// give rather than something inferred from the absence of an error.
func (d *httpDialect) RuntimeStatus(ctx context.Context, api httpagent.API) (string, error) {
	var usage providerUsage
	if err := api(ctx, "GET", "/kilocode/provider-usage", nil, &usage); err != nil {
		return "", err
	}
	return formatProviderUsage(usage), nil
}

// formatProviderUsage renders one line per plan the engine knows about.
func formatProviderUsage(u providerUsage) string {
	if len(u.Items) == 0 {
		// An account with no plan windows is the normal shape for a
		// bring-your-own-key setup: kilo has nothing to meter because the spend
		// is on the upstream provider's own bill.
		return "Kilo · no plan usage reported (this account meters upstream)"
	}

	var parts []string
	for _, item := range u.Items {
		label := strings.TrimSpace(item.ProviderLabel)
		if label == "" {
			label = item.ProviderID
		}
		switch item.FetchState {
		case "error", "unavailable":
			parts = append(parts, label+" (usage unavailable)")
			continue
		}
		if plan := strings.TrimSpace(item.PlanLabel); plan != "" {
			label += " " + plan
		}
		parts = append(parts, label+windowSummary(item))
	}
	return "Kilo · " + strings.Join(parts, " · ")
}

// windowSummary describes a plan's windows, preferring what is spent.
func windowSummary(item providerUsageSnapshot) string {
	var spent, live []string
	for _, w := range item.Windows {
		name := strings.TrimSpace(w.Resource)
		if name == "" {
			name = "usage"
		}
		switch w.State {
		case "exhausted":
			spent = append(spent, name+" exhausted")
		case "unlimited", "not_in_plan":
			// Nothing to report: an unlimited window has no number worth
			// showing, and one outside the plan is not this account's business.
		default:
			if w.Limit > 0 {
				live = append(live, fmt.Sprintf("%s %.0f/%.0f %s", name, w.Used, w.Limit, w.Unit))
			}
		}
	}
	all := append(spent, live...)
	if len(all) == 0 {
		return ""
	}
	return ": " + strings.Join(all, ", ")
}
