package acpagent

import (
	"fmt"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// xaiCompactionStatus maps grok's four compaction variants on
// `_x.ai/session_notification` to ACP's compaction status (MADR 0208, P4b
// deviation 1). grok compacts on `/compact` and on a full context window, and
// reports it only here: the standard compaction_update is never sent.
var xaiCompactionStatus = map[string]acp.CompactionStatus{
	"auto_compact_started":   acp.CompactionStatusInProgress,
	"auto_compact_completed": acp.CompactionStatusCompleted,
	"auto_compact_failed":    acp.CompactionStatusFailed,
	"auto_compact_cancelled": acp.CompactionStatusCancelled,
}

// onXAICompaction folds one of grok's compaction variants into the same state
// ACP's compaction_update produces, and emits it (MADR 0190 D1's shape).
//
// grok's variants carry no ID, so Magic numbers them. `started` opens one, and
// the next terminal variant closes it. A terminal variant with none open is a
// whole compaction on its own: grok's `/compact` sends `auto_compact_completed`
// alone, with no `started` before it.
//
// The summary is grok's `summary_preview` and the error is grok's `error`, as
// sent. Nothing of Magic's own wording is added (MADR 0208 D11).
func onXAICompaction(h Host, variant, errText string, preview *string) {
	status, ok := xaiCompactionStatus[variant]
	if !ok {
		return
	}
	st := grokState(h)
	id := st.compactionOpen
	if id == "" {
		st.compactions++
		id = fmt.Sprintf("xai-%d", st.compactions)
	}
	if status == acp.CompactionStatusInProgress {
		st.compactionOpen = id
	} else {
		st.compactionOpen = ""
	}
	h.UpdateCompaction(id, status, preview, errText)
}
