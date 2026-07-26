// Package acpcommon holds pure conversions shared by ACP transports.
package acpcommon

import (
	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// PlanEntries converts ACP plan entries to the daemon's fixed work-item
// vocabulary. Unknown values are deliberately made safe defaults so every
// provider produces values the protocol and mobile UI understand.
func PlanEntries(in []acp.PlanEntry) []event.PlanEntry {
	out := make([]event.PlanEntry, 0, len(in))
	for _, entry := range in {
		out = append(out, event.PlanEntry{
			Content:  entry.Content,
			Status:   planStatus(entry.Status),
			Priority: planPriority(entry.Priority),
		})
	}
	return out
}

func planStatus(status acp.PlanEntryStatus) string {
	switch status {
	case acp.PlanEntryStatusPending:
		return event.PlanStatusPending
	case acp.PlanEntryStatusInProgress:
		return event.PlanStatusInProgress
	case acp.PlanEntryStatusCompleted:
		return event.PlanStatusCompleted
	default:
		return event.PlanStatusPending
	}
}

func planPriority(priority acp.PlanEntryPriority) string {
	switch priority {
	case acp.PlanEntryPriorityHigh:
		return event.PlanPriorityHigh
	case acp.PlanEntryPriorityMedium:
		return event.PlanPriorityMedium
	case acp.PlanEntryPriorityLow:
		return event.PlanPriorityLow
	default:
		return event.PlanPriorityMedium
	}
}
