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
