package acpagent

import (
	"fmt"
	"strings"
	"time"

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
func (s *session) onXAICompaction(variant, errText string, preview *string, now time.Time) {
	status, ok := xaiCompactionStatus[variant]
	if !ok {
		return
	}
	id := s.xaiCompactionOpen
	if id == "" {
		s.xaiCompactions++
		id = acp.CompactionId(fmt.Sprintf("xai-%d", s.xaiCompactions))
	}
	e, _ := s.compactions.entry(id)
	e.status = string(status)
	if status == acp.CompactionStatusInProgress {
		s.xaiCompactionOpen = id
	} else {
		s.xaiCompactionOpen = ""
	}
	if preview != nil {
		e.summary, e.truncated = e.summary[:0], false
		e.appendSummary(strings.TrimSpace(*preview))
	}
	if status == acp.CompactionStatusFailed {
		e.err = strings.TrimSpace(errText)
	}
	s.emitCompaction(id, e, now)
}
