package kilo

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestKiloImplementsRuntimeDialect(t *testing.T) {
	var d any = &httpDialect{}
	if _, ok := d.(httpagent.RuntimeDialect); !ok {
		t.Fatal("kilo's dialect does not implement httpagent.RuntimeDialect")
	}
}

// TestKiloStatusReportsWindowsAndExhaustion.
//
// kilo says more here than any other engine in this repository: its windows
// carry an explicit per-resource state, so "not throttled" is an answer rather
// than an inference from the absence of an error.
func TestKiloStatusReportsWindowsAndExhaustion(t *testing.T) {
	u := providerUsage{Items: []providerUsageSnapshot{{
		ProviderID: "kilocode", ProviderLabel: "Kilo Code", PlanLabel: "Pro",
		FetchState: "ok",
		Windows: []providerUsageWindow{
			{Resource: "credits", Unit: "USD", Used: 12, Limit: 50, State: "active"},
			{Resource: "requests", State: "exhausted"},
			{Resource: "images", State: "not_in_plan"},
			{Resource: "tokens", State: "unlimited"},
		},
	}}}

	got := formatProviderUsage(u)
	for _, want := range []string{"Kilo Code Pro", "requests exhausted", "credits 12/50 USD"} {
		if !strings.Contains(got, want) {
			t.Errorf("status = %q, want %q", got, want)
		}
	}
	// A window outside the plan and an unlimited one carry no number worth
	// showing, and padding the line with them makes the real figures harder to
	// find.
	for _, unwanted := range []string{"images", "tokens"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("status = %q, should omit the %q window", got, unwanted)
		}
	}
}

// TestKiloStatusOnAnAccountWithNoPlan.
//
// An empty items list is the normal shape for a bring-your-own-key setup: kilo
// meters nothing because the spend is on the upstream provider's bill. Saying
// that is different from saying usage is unavailable.
func TestKiloStatusOnAnAccountWithNoPlan(t *testing.T) {
	got := formatProviderUsage(providerUsage{})
	if !strings.Contains(got, "meters upstream") {
		t.Errorf("status = %q, want it to explain why there is no plan usage", got)
	}
	if strings.Contains(strings.ToLower(got), "unavailable") {
		t.Errorf("status = %q — an account with no plan is not an unavailable one", got)
	}
}

// TestKiloStatusSaysWhenTheEngineCouldNotFetch keeps a failed per-provider
// fetch distinct from an account that has no plan.
func TestKiloStatusSaysWhenTheEngineCouldNotFetch(t *testing.T) {
	u := providerUsage{Items: []providerUsageSnapshot{
		{ProviderID: "anthropic", ProviderLabel: "Anthropic", FetchState: "error"},
	}}
	got := formatProviderUsage(u)
	if !strings.Contains(got, "usage unavailable") {
		t.Errorf("status = %q, want the failed fetch named", got)
	}
}
