package gobble

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// What gobble sends beside the standard updates (MADR 0179 D10, A1, A2).
// gobble's compactions themselves arrive as the standard compaction_update,
// which acpagent shows; its _gobble/compaction goes only to a client that did
// not offer session.compaction, and this package registers nothing for it.

// handleRetry tells the phone about a turn's retry wait: when it starts, with
// the attempt, the wait and the cause, and when it ends with an error. A
// successful end says nothing. The cause is named as gobble sent it: it is an
// open set, and a cause a later gobble adds is shown the same way (A1).
func handleRetry(_ context.Context, h acpagent.Host, params json.RawMessage) {
	var n acpext.RetryNotice
	if err := json.Unmarshal(params, &n); err != nil {
		h.Decline(acpext.MethodRetry)
		return
	}
	switch n.Phase {
	case acpext.PhaseStart:
		h.Notice(retryStartText(n))
	case acpext.PhaseEnd:
		if n.Error != "" {
			h.Notice(fmt.Sprintf("gobble stopped retrying: %s", strings.TrimSpace(n.Error)))
		}
	}
}

// retryStartText is a retry start's notice.
func retryStartText(n acpext.RetryNotice) string {
	var b strings.Builder
	b.WriteString("gobble is retrying")
	if n.Attempt > 0 {
		fmt.Fprintf(&b, " (attempt %d", n.Attempt)
		if n.MaxAttempts > 0 {
			fmt.Fprintf(&b, " of %d", n.MaxAttempts)
		}
		b.WriteString(")")
	}
	if n.DelayMs > 0 {
		fmt.Fprintf(&b, " in %s", (time.Duration(n.DelayMs) * time.Millisecond).Round(time.Second/10))
	}
	if cause := strings.TrimSpace(string(n.Cause)); cause != "" {
		fmt.Fprintf(&b, ": %s", cause)
	}
	b.WriteString(".")
	return b.String()
}

// handleCompactionMeta reads gobble's facts about a compaction, its
// CompactionMeta under _meta.gobble of a compaction_update, which acpagent
// passes after the compaction's own event (A2). A live completed compaction
// carries both a reason and the tokens before it, and gets one notice naming
// them. Nothing else does: an in-progress or failed update's state is on the
// compaction card already, a replayed completed one (no reason) must not
// notify again, and a pause is gobble's own warning notice.
func handleCompactionMeta(_ context.Context, h acpagent.Host, from acpagent.MetaSource, value json.RawMessage) {
	if from != acpagent.MetaFromCompaction {
		h.Decline("_meta." + acpext.MetaKey + "." + string(from))
		return
	}
	var m acpext.CompactionMeta
	if err := json.Unmarshal(value, &m); err != nil {
		h.Decline("compaction_update._meta." + acpext.MetaKey)
		return
	}
	reason := strings.TrimSpace(string(m.Reason))
	if reason == "" || m.TokensBefore <= 0 {
		return
	}
	h.Notice(fmt.Sprintf("Context compacted (%s), from %d tokens.", reason, m.TokensBefore))
}
