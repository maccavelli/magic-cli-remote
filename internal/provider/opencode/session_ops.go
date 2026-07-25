package opencode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Fork creates a new OpenCode session branched at messageID (optional).
// Implements provider.ForkSession on the httpagent host session via dialect
// methods invoked through the host API.
func (o *httpSession) Fork(ctx context.Context, messageID string) (string, error) {
	body := map[string]any{}
	if messageID != "" {
		body["messageID"] = messageID
	}
	var out struct {
		ID string `json:"id"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/fork"+o.dir(), body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("opencode fork: empty session id")
	}
	return out.ID, nil
}

// Revert undoes a message (OpenCode POST …/revert).
func (o *httpSession) Revert(ctx context.Context, messageID, partID string) error {
	if messageID == "" {
		return fmt.Errorf("message_id required")
	}
	body := map[string]any{"messageID": messageID}
	if partID != "" {
		body["partID"] = partID
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/revert"+o.dir(), body, nil)
}

// Unrevert restores reverted messages (OpenCode POST …/unrevert).
func (o *httpSession) Unrevert(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/unrevert"+o.dir(), nil, nil)
}

// Compact summarises the conversation server-side (OpenCode POST …/summarize),
// backing the canonical /compact. The engine needs an explicit model for the
// summarisation pass, so this reuses the session's resolved model. The summary
// itself arrives over SSE like any other assistant message.
//
// The v2 route (POST /api/session/{id}/compact) is not used: on engine 1.18.5 it
// answers 503 "Session compact is not available yet".
func (o *httpSession) Compact(ctx context.Context) error {
	mp, mid := o.resolveModel()
	if mp == "" || mid == "" {
		return fmt.Errorf("opencode compact: no model resolved for this session")
	}
	body := map[string]any{"providerID": mp, "modelID": mid}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/summarize"+o.dir(), body, nil)
}

// SetModel repoints the session at another model without restarting anything
// (OpenCode POST /api/session/{id}/model), backing the canonical /model. The
// host's own model string is updated too, so later prompts and a /compact pass
// agree with the engine.
func (o *httpSession) SetModel(ctx context.Context, model string) error {
	mp, mid := splitModel(model)
	if mp == "" || mid == "" {
		return fmt.Errorf("opencode set model: %q is not a provider/model id", model)
	}
	// ModelRef is {providerID, id} here — the same shape session create uses,
	// not the {providerID, modelID} that prompt and summarize take.
	body := map[string]any{"model": map[string]string{"providerID": mp, "id": mid}}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// No ?directory=: unlike the legacy routes, this v2 endpoint declares only
	// the session id, and session ids are engine-global.
	if err := o.h.API()(callCtx, "POST",
		"/api/session/"+o.h.AgentSessionID()+"/model", body, nil); err != nil {
		return err
	}
	o.h.RecordModel(mp + "/" + mid)
	return nil
}

// UndoLast reverts the most recent turn, backing the canonical /undo. Revert
// needs a provider-native message id that the daemon never sees, so the last
// user message is resolved here.
func (o *httpSession) UndoLast(ctx context.Context) (string, error) {
	var msgs []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"info"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := o.h.API()(callCtx, "GET",
		"/session/"+o.h.AgentSessionID()+"/message"+o.dir(), nil, &msgs); err != nil {
		return "", err
	}
	last := ""
	for _, m := range msgs {
		if m.Info.Role == "user" && m.Info.ID != "" {
			last = m.Info.ID
		}
	}
	if last == "" {
		return "", fmt.Errorf("nothing to undo in this session")
	}
	if err := o.Revert(ctx, last, ""); err != nil {
		return "", err
	}
	return "reverted the last turn", nil
}

// Diff fetches GET …/diff and returns a short summary string.
func (o *httpSession) Diff(ctx context.Context, messageID string) (string, error) {
	path := "/session/" + o.h.AgentSessionID() + "/diff" + o.dir()
	if messageID != "" {
		// Append messageID query; dir() already starts with ?.
		if strings.Contains(path, "?") {
			path += "&messageID=" + messageID
		} else {
			path += "?messageID=" + messageID
		}
	}
	var diffs []struct {
		File      string `json:"file"`
		Path      string `json:"path"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := o.h.API()(callCtx, "GET", path, nil, &diffs); err != nil {
		return "", err
	}
	if len(diffs) == 0 {
		return "No file changes", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diff · %d file(s)\n", len(diffs))
	const maxFiles = 20
	for i, d := range diffs {
		if i >= maxFiles {
			fmt.Fprintf(&b, "…and %d more\n", len(diffs)-maxFiles)
			break
		}
		path := firstNonEmpty(d.File, d.Path, "?")
		st := d.Status
		if st == "" {
			st = "modified"
		}
		fmt.Fprintf(&b, "  %s  +%d −%d  %s\n", st, d.Additions, d.Deletions, path)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Silence unused import if provider helpers are only used for docs.
var _ = provider.ErrTurnBusy
