package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// methodChildUpdate is opencode's notification for a child session's progress
// and output, sent when the client advertises childSessionUpdatesKey
// (packages/cli/src/acp/translate.ts:418-428 at b44b7164ba). Its name has no
// leading `_`, so acpagent delivers it through the SDK's receiving middleware
// (0214 MADR D14, corrected).
const methodChildUpdate = "opencode/session/child_update"

// metaRetry is opencode's retry marker, on session_info_update and on the
// prompt result (translate.ts:19, :166, :265, :275).
const metaRetry = "opencode/retry"

// childUpdate is one opencode/session/child_update notification. G1 recorded
// both kinds: "status" (created, running, completed; also failed and
// interrupted in the source) and "update", whose update is the child's own
// session update.
type childUpdate struct {
	ChildSessionID string          `json:"childSessionId"`
	Title          string          `json:"title"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	Update         json.RawMessage `json:"update"`
}

// handleChildUpdate puts opencode's child sessions in the subagent panel
// (0214 MADR D5). A child's own output stays out of the parent's transcript:
// it reports to the parent agent, whose reply carries the conclusion, as grok's
// subagents do (MADR 0051 D6). An update only says the child is running.
func handleChildUpdate(_ context.Context, h acpagent.Host, params json.RawMessage) {
	var u childUpdate
	if err := json.Unmarshal(params, &u); err != nil || u.ChildSessionID == "" {
		h.Log().Debug("opencode: undecodable child_update", "err", err)
		return
	}
	status := event.SubagentStatusRunning
	if u.Type == "status" {
		switch u.Status {
		case "completed":
			status = event.SubagentStatusCompleted
		case "failed", "interrupted":
			status = event.SubagentStatusFailed
		}
	}
	h.NoteSubagent(u.ChildSessionID, "", u.Title, status)
}

// retryMarker is the value of metaRetry: a scheduled retry, or JSON null when
// the next step started (translate.ts:265-275; SessionStructuredError in
// packages/client/src/promise/generated/types.ts:114-120).
type retryMarker struct {
	Attempt     int       `json:"attempt"`
	NextRetryAt time.Time `json:"nextRetryAt"`
	Error       struct {
		Message      string `json:"message"`
		RetryAfterMs int64  `json:"retryAfterMs"`
	} `json:"error"`
}

// handleRetry shows opencode's retries (0214 MADR D6), by the rule the 1.x
// dialect's handleSessionStatus uses (lifecycle.go): a quota error or a long
// back-off ends the turn with the limit card and its retry time, so the phone
// is not left "running" while opencode sleeps; a short retry is a notice. A
// cleared marker says nothing. On a prompt result the turn is already over, so
// only a limit is reported.
func handleRetry(_ context.Context, h acpagent.Host, from acpagent.MetaSource, value json.RawMessage) {
	if v := strings.TrimSpace(string(value)); v == "" || v == "null" {
		return
	}
	var m retryMarker
	if err := json.Unmarshal(value, &m); err != nil {
		h.Log().Debug("opencode: undecodable retry marker", "err", err)
		return
	}
	now := time.Now()
	msg := strings.TrimSpace(m.Error.Message)
	if msg == "" {
		msg = "the model request failed"
	}
	wait := time.Duration(m.Error.RetryAfterMs) * time.Millisecond
	if !m.NextRetryAt.IsZero() {
		wait = m.NextRetryAt.Sub(now)
	}
	cls := agenterr.Present(msg, now)
	if cls.Kind == agenterr.KindQuota || wait >= agenterr.LongBackoffMin || agenterr.LooksLikeLongBackoff(msg) {
		retryAt := cls.ResetAt
		if retryAt.IsZero() && wait > 0 {
			retryAt = now.Add(wait)
		}
		out := cls.Message
		if out == "" {
			out = msg
		}
		h.EndTurnWithLimit(out, retryAt)
		return
	}
	if from == acpagent.MetaFromPromptResult {
		return
	}
	note := fmt.Sprintf("Retrying (attempt %d): %s", m.Attempt, msg)
	if wait > 0 {
		note = fmt.Sprintf("Retrying in %s (attempt %d): %s", wait.Round(time.Second), m.Attempt, msg)
	}
	h.Notice(note)
}
