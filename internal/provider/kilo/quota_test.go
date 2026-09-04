package kilo

import (
	"strings"
	"testing"
)

func TestExhaustedWindowsSummarisesWhatTheEngineReports(t *testing.T) {
	u := providerUsage{
		GeneratedAt: "2026-09-04T02:58:47.320Z",
		Items: []providerUsageSnapshot{
			{
				ProviderID:    "kilo",
				ProviderLabel: "Kilo",
				PlanLabel:     "Pro",
				FetchState:    "ready",
				PlanState:     "active",
				Windows: []providerUsageWindow{
					{Resource: "requests", State: "active", Used: 10, Limit: 100},
					{Resource: "monthly credits", State: "exhausted", ResetAt: "2026-10-01T00:00:00Z"},
				},
			},
		},
	}
	got := u.exhaustedWindows()
	if !strings.Contains(got, "Kilo") || !strings.Contains(got, "monthly credits") {
		t.Fatalf("summary = %q, want the provider and the exhausted resource", got)
	}
	if !strings.Contains(got, "2026-10-01") {
		t.Fatalf("summary = %q, want the reset time the engine gave", got)
	}
	if strings.Contains(got, "requests") {
		t.Fatalf("summary = %q, must not report a window that is still active", got)
	}
}

func TestExhaustedWindowsIgnoresUnreadablePlans(t *testing.T) {
	// A plan the engine could not fetch says nothing about whether a limit was
	// hit. Reporting it as exhausted would invent a fact.
	u := providerUsage{Items: []providerUsageSnapshot{
		{ProviderID: "p", FetchState: "error", Windows: []providerUsageWindow{{Resource: "x", State: "exhausted"}}},
		{ProviderID: "q", FetchState: "unavailable", Windows: []providerUsageWindow{{Resource: "y", State: "exhausted"}}},
	}}
	if got := u.exhaustedWindows(); got != "" {
		t.Fatalf("summary = %q, want empty for unreadable plans", got)
	}
}

func TestExhaustedWindowsIsEmptyWhenNothingIsExhausted(t *testing.T) {
	// This is the shape the operator's own account returns today:
	// {"items":[],"generatedAt":"..."}.
	if got := (providerUsage{}).exhaustedWindows(); got != "" {
		t.Fatalf("summary = %q, want empty", got)
	}
	u := providerUsage{Items: []providerUsageSnapshot{{
		ProviderID: "kilo", FetchState: "ready",
		Windows: []providerUsageWindow{{Resource: "credits", State: "unlimited"}},
	}}}
	if got := u.exhaustedWindows(); got != "" {
		t.Fatalf("summary = %q, want empty", got)
	}
}

func TestAnnotateLimitLeavesAnUnconfirmedMessageAlone(t *testing.T) {
	const msg = "Monthly usage limit reached."
	if got := annotateLimit(msg, ""); got != msg {
		t.Fatalf("annotateLimit with no summary changed the message: %q", got)
	}
	got := annotateLimit(msg, "Kilo monthly credits")
	if !strings.Contains(got, "Monthly usage limit reached") || !strings.Contains(got, "Kilo monthly credits") {
		t.Fatalf("annotated message lost information: %q", got)
	}
}
