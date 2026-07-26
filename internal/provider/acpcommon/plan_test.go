package acpcommon

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestPlanEntriesNormalizesVocabulary(t *testing.T) {
	got := PlanEntries([]acp.PlanEntry{
		{Content: "pending", Status: acp.PlanEntryStatusPending, Priority: acp.PlanEntryPriorityHigh},
		{Content: "working", Status: acp.PlanEntryStatusInProgress, Priority: acp.PlanEntryPriorityMedium},
		{Content: "done", Status: acp.PlanEntryStatusCompleted, Priority: acp.PlanEntryPriorityLow},
		{Content: "unknown", Status: acp.PlanEntryStatus("new"), Priority: acp.PlanEntryPriority("urgent")},
	})
	want := []event.PlanEntry{
		{Content: "pending", Status: event.PlanStatusPending, Priority: event.PlanPriorityHigh},
		{Content: "working", Status: event.PlanStatusInProgress, Priority: event.PlanPriorityMedium},
		{Content: "done", Status: event.PlanStatusCompleted, Priority: event.PlanPriorityLow},
		{Content: "unknown", Status: event.PlanStatusPending, Priority: event.PlanPriorityMedium},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPlanEntriesEmptyIsNonNil(t *testing.T) {
	got := PlanEntries(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("PlanEntries(nil) = %#v, want non-nil empty", got)
	}
}
