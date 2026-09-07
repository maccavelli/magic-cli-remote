package acpagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// grok's rewind surface, transcribed from
// ~/gitrepos/grok-build/crates/codegen/xai-grok-shell/src/session/acp_types.rs
// and .../extensions/rewind.rs (MADR 0138 Phase 9).
//
// The response types carry **no** `#[serde(rename_all)]`, so they arrive in
// snake_case — unlike `_x.ai/session/fork`, whose response on the same
// transport is camelCase. Two vendor methods, two conventions; assuming either
// one produces a struct full of zero values and no error.

// rewindModeAll rolls back the conversation *and* the files.
//
// It is sent explicitly on every call, never omitted. `RewindRequest.mode`
// carries the comment "Clients must specify this explicitly. Defaults to `All`
// for backwards compatibility with older clients" — so omitting it happens to
// mean the same thing today and would silently change meaning if grok ever
// moved the default.
//
// `All` is also what `/undo` already means elsewhere: kilo's UndoLast reverts
// the last user message through an endpoint its engine documents as "undoing
// its effects and restoring the previous state". One button, one meaning.
const rewindModeAll = "all"

type rewindPointsRequest struct {
	SessionID string `json:"sessionId"`
}

type rewindPointsResponse struct {
	RewindPoints []rewindPoint `json:"rewind_points"`
}

type rewindPoint struct {
	PromptIndex    int    `json:"prompt_index"`
	CreatedAt      string `json:"created_at"`
	NumFileSnaps   int    `json:"num_file_snapshots"`
	HasFileChanges bool   `json:"has_file_changes"`
	PromptPreview  string `json:"prompt_preview"`
}

type rewindExecuteRequest struct {
	SessionID         string `json:"sessionId"`
	TargetPromptIndex int    `json:"targetPromptIndex"`
	// Force is always false. A conflict means the working tree diverged from
	// the snapshot — the operator edited files since the turn ran — and forcing
	// discards those edits. The conflicts are reported instead.
	Force bool   `json:"force"`
	Mode  string `json:"mode"`
}

type rewindExecuteResponse struct {
	Success           bool             `json:"success"`
	TargetPromptIndex int              `json:"target_prompt_index"`
	Mode              string           `json:"mode"`
	RevertedFiles     []string         `json:"reverted_files"`
	CleanFiles        []string         `json:"clean_files"`
	Conflicts         []rewindConflict `json:"conflicts"`
	PromptText        string           `json:"prompt_text"`
	Error             string           `json:"error"`
}

type rewindConflict struct {
	Path string `json:"path"`
	// Kind is one of missing_file, extra_file, content_mismatch.
	Kind string `json:"conflict_type"`
}

var _ provider.UndoSession = (*session)(nil)

// UndoLast implements [provider.UndoSession] over grok's rewind extension.
//
// UndoSession and not RevertSession: Revert takes a provider-native message id,
// and grok emits none — its rewind is indexed by prompt position. UndoSession
// is defined as resolving "the last turn" itself, which is what the two calls
// below do.
//
// There is deliberately no Unrevert. grok has no un-rewind, so `/redo` stays
// unavailable on it rather than being wired to something that only resembles
// one.
func (s *session) UndoLast(ctx context.Context) (string, error) {
	agentID := s.AgentSessionID()
	if agentID == "" {
		return "", errors.New("session has no agent session id")
	}

	var points rewindPointsResponse
	if err := callAgentExtension(ctx, s, "x.ai/rewind/points",
		rewindPointsRequest{SessionID: agentID}, &points); err != nil {
		return "", err
	}
	target, ok := lastRewindPoint(points.RewindPoints)
	if !ok {
		// Same wording as kilo's for the same state, so the phone shows one
		// message regardless of provider.
		return "", errors.New("nothing to undo in this session")
	}

	var res rewindExecuteResponse
	if err := callAgentExtension(ctx, s, "x.ai/rewind/execute", rewindExecuteRequest{
		SessionID:         agentID,
		TargetPromptIndex: target.PromptIndex,
		Force:             false,
		Mode:              rewindModeAll,
	}, &res); err != nil {
		return "", err
	}
	if !res.Success {
		return "", rewindFailure(res)
	}
	return rewindSummary(res, target), nil
}

// lastRewindPoint returns the most recent checkpoint.
//
// By highest PromptIndex rather than by position: the field is documented as
// monotonically increasing per session, which is a property worth relying on
// explicitly instead of assuming the array is ordered.
func lastRewindPoint(points []rewindPoint) (rewindPoint, bool) {
	var best rewindPoint
	found := false
	for _, p := range points {
		if !found || p.PromptIndex > best.PromptIndex {
			best, found = p, true
		}
	}
	return best, found
}

// rewindFailure turns an unsuccessful rewind into an error the operator can act
// on.
//
// A conflict list is the useful case: it names the files that changed since the
// snapshot, which is what the operator has to reconcile before an undo can be
// safe. Nothing was reverted.
func rewindFailure(res rewindExecuteResponse) error {
	if len(res.Conflicts) > 0 {
		paths := make([]string, 0, len(res.Conflicts))
		for _, c := range res.Conflicts {
			if c.Kind != "" {
				paths = append(paths, fmt.Sprintf("%s (%s)", c.Path, c.Kind))
				continue
			}
			paths = append(paths, c.Path)
		}
		return fmt.Errorf("undo would conflict with changes made since that turn; "+
			"nothing was reverted: %s", strings.Join(paths, ", "))
	}
	if msg := strings.TrimSpace(res.Error); msg != "" {
		return errors.New(msg)
	}
	return errors.New("undo failed")
}

// rewindSummary says what was undone, not merely that something was.
func rewindSummary(res rewindExecuteResponse, target rewindPoint) string {
	var b strings.Builder
	switch n := len(res.RevertedFiles); {
	case n == 1:
		b.WriteString("Undid the last turn and reverted 1 file")
	case n > 1:
		fmt.Fprintf(&b, "Undid the last turn and reverted %d files", n)
	default:
		b.WriteString("Undid the last turn; no files had changed")
	}
	if preview := strings.TrimSpace(target.PromptPreview); preview != "" {
		fmt.Fprintf(&b, " (%q)", truncateRunes(preview, 80))
	}
	return b.String()
}
