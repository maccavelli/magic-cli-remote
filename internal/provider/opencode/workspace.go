package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Workspace bounds (MADR 0112 A5, PLAN P6 cross-cutting limits).
const (
	maxWorkspacePathBytes = 4096
	maxWorkspaceEntries   = 200
	maxWorkspaceQueryLen  = 256
	maxWorkspaceMatches   = 100
	maxWorkspaceFileBytes = 262144
	// maxWorkspaceEnvelope is the largest raw JSON body this surface accepts.
	// One extra byte is read to detect overflow *before* decoding, so an
	// oversized response fails outright instead of decoding to partial JSON.
	maxWorkspaceEnvelope = 2097152
	// fileSearchLimit is what /find/file is asked for. The schema accepts
	// 1..200 and the handler defaults to 10.
	fileSearchLimit = 100
	// textSearchCap is hard-coded in the 1.18.21 findText handler. There is no
	// request parameter to raise it, so it is reported rather than assumed
	// equal to the row budget.
	textSearchCap = 10
)

// Workspace errors. These map onto stable protocol codes; the wire layer
// translates them rather than copying an upstream body.
var (
	errWorkspaceInvalidPath  = errors.New("invalid_path")
	errWorkspacePathEscape   = errors.New("path_escape")
	errWorkspacePathSymlink  = errors.New("path_symlink")
	errWorkspaceBinary       = errors.New("binary_content")
	errWorkspaceTooLarge     = errors.New("result_too_large")
	errWorkspaceInvalidQuery = errors.New("invalid_query")
)

var _ interface {
	ListWorkspace(context.Context, string) ([]provider.WorkspaceEntry, error)
	ReadWorkspace(context.Context, string) (provider.WorkspaceContent, error)
	SearchWorkspace(context.Context, string, string) (provider.WorkspaceSearch, error)
} = (*httpSession)(nil)

// normalizeWorkspacePath validates a caller-supplied relative path and returns
// its cleaned slash form ("" for the root).
//
// Absolute paths, NUL, traversal beyond the root and over-long input are
// rejected outright. This runs before any filesystem or HTTP work so a bad
// path never reaches either.
func normalizeWorkspacePath(in string) (string, error) {
	if len(in) > maxWorkspacePathBytes {
		return "", fmt.Errorf("%w: path exceeds %d bytes", errWorkspaceInvalidPath, maxWorkspacePathBytes)
	}
	if strings.ContainsRune(in, 0) {
		return "", fmt.Errorf("%w: path contains NUL", errWorkspaceInvalidPath)
	}
	if !utf8.ValidString(in) {
		return "", fmt.Errorf("%w: path is not valid UTF-8", errWorkspaceInvalidPath)
	}
	// Accept either separator from a client, then work in slash form.
	s := strings.ReplaceAll(in, "\\", "/")
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == "./" {
		return "", nil
	}
	if strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("%w: absolute paths are not accepted", errWorkspaceInvalidPath)
	}
	// A scheme-ish prefix is a URI, not a workspace path.
	if strings.Contains(s, "://") {
		return "", fmt.Errorf("%w: URIs are not accepted", errWorkspaceInvalidPath)
	}
	cleaned := path.Clean(s)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: path escapes the session root", errWorkspacePathEscape)
	}
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

// checkNoSymlinkComponents rejects any *existing* component beneath root that
// is a symlink.
//
// Rejecting rather than resolving is the point: a resolved link is trusted, and
// the thing it points at can change between the check and the engine's open.
// OpenCode — not this daemon — performs that open, so this cannot eliminate a
// race with a concurrent local filesystem actor. The supported threat boundary
// assumes the engine's own workspace is not concurrently hostile; what this
// does eliminate is a link that already exists at validation time
// (MADR 0112 A5).
func checkNoSymlinkComponents(root, rel string) error {
	if rel == "" {
		return nil
	}
	cur := root
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				// A component that does not exist cannot be a symlink; the
				// engine will report the miss.
				return nil
			}
			return fmt.Errorf("%w: cannot inspect path", errWorkspaceInvalidPath)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is a symlink", errWorkspacePathSymlink, seg)
		}
	}
	return nil
}

// validateWorkspacePath performs the full pre-dispatch check.
func (o *httpSession) validateWorkspacePath(in string) (string, error) {
	rel, err := normalizeWorkspacePath(in)
	if err != nil {
		return "", err
	}
	if err := checkNoSymlinkComponents(o.h.CWD(), rel); err != nil {
		return "", err
	}
	return rel, nil
}

// validateReturnedPath re-checks a path the engine returned.
//
// Upstream is not trusted to stay inside the root either: a listing that
// escaped would otherwise become a navigable rung out of the workspace.
func validateReturnedPath(in string) (string, bool) {
	rel, err := normalizeWorkspacePath(strings.TrimSuffix(in, "/"))
	if err != nil || rel == "" {
		return "", false
	}
	return rel, true
}

// workspaceGet performs one bounded workspace request.
//
// It deliberately does not use the shared engine helper: that one allows a
// 16 MiB body and would silently accept a response far larger than this
// surface's envelope. Reading one byte past the cap turns "too large" into a
// detectable condition rather than truncated JSON (PLAN P6 step 3).
func (o *httpSession) workspaceGet(ctx context.Context, route string, params url.Values, out any) error {
	params.Set("directory", o.h.CWD())
	var raw json.RawMessage
	if err := o.h.API()(ctx, "GET", route+"?"+params.Encode(), nil, &raw); err != nil {
		return err
	}
	if len(raw) > maxWorkspaceEnvelope {
		return fmt.Errorf("%w: response exceeds %d bytes", errWorkspaceTooLarge, maxWorkspaceEnvelope)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("workspace response could not be decoded")
	}
	return nil
}

// boundedJSONReader enforces the envelope on a streaming body.
//
// Exposed for the transport to use where a reader is available; it reads one
// byte past the cap so overflow is detected before any decode is attempted.
func boundedJSONReader(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxWorkspaceEnvelope+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxWorkspaceEnvelope {
		return nil, fmt.Errorf("%w: response exceeds %d bytes", errWorkspaceTooLarge, maxWorkspaceEnvelope)
	}
	return b, nil
}

// ListWorkspace implements [provider.WorkspaceSession].
func (o *httpSession) ListWorkspace(ctx context.Context, reqPath string) ([]provider.WorkspaceEntry, error) {
	rel, err := o.validateWorkspacePath(reqPath)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if rel == "" {
		q.Set("path", ".")
	} else {
		q.Set("path", rel)
	}
	var upstream []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		// Absolute is decoded only so it is visibly discarded; it never
		// reaches an entry.
		Absolute string `json:"absolute"`
		Ignored  bool   `json:"ignored"`
	}
	if err := o.workspaceGet(ctx, "/file", q, &upstream); err != nil {
		return nil, err
	}
	out := make([]provider.WorkspaceEntry, 0, len(upstream))
	for _, u := range upstream {
		p, ok := validateReturnedPath(u.Path)
		if !ok {
			continue
		}
		name := strings.TrimSpace(u.Name)
		if name == "" {
			name = path.Base(p)
		}
		out = append(out, provider.WorkspaceEntry{
			Name:    name,
			Path:    p,
			Dir:     u.Type == "directory",
			Ignored: u.Ignored,
		})
	}
	// Directories first, then lexical by path: a stable order the client can
	// render without sorting, and one that does not depend on engine order.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > maxWorkspaceEntries {
		out = out[:maxWorkspaceEntries]
	}
	return out, nil
}

// ReadWorkspace implements [provider.WorkspaceSession].
func (o *httpSession) ReadWorkspace(ctx context.Context, reqPath string) (provider.WorkspaceContent, error) {
	rel, err := o.validateWorkspacePath(reqPath)
	if err != nil {
		return provider.WorkspaceContent{}, err
	}
	if rel == "" {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: no file named", errWorkspaceInvalidPath)
	}
	q := url.Values{}
	q.Set("path", rel)
	var upstream struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := o.workspaceGet(ctx, "/file/content", q, &upstream); err != nil {
		return provider.WorkspaceContent{}, err
	}
	// Upstream labels binary content explicitly; raw bytes and base64 are never
	// forwarded, so a binary file is a refusal rather than a garbled view.
	if upstream.Type != "" && upstream.Type != "text" {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: %q content is not text", errWorkspaceBinary, upstream.Type)
	}
	if strings.ContainsRune(upstream.Content, 0) {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: content contains NUL", errWorkspaceBinary)
	}
	if !utf8.ValidString(upstream.Content) {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: content is not valid UTF-8", errWorkspaceBinary)
	}
	// Oversize is a refusal, never a partial view: a silently truncated file
	// read as complete is how a reviewer misses the part that mattered.
	if len(upstream.Content) > maxWorkspaceFileBytes {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: file exceeds %d bytes", errWorkspaceTooLarge, maxWorkspaceFileBytes)
	}
	return provider.WorkspaceContent{
		Path:  rel,
		Text:  upstream.Content,
		Bytes: len(upstream.Content),
	}, nil
}

// SearchWorkspace implements [provider.WorkspaceSession].
func (o *httpSession) SearchWorkspace(ctx context.Context, kind, query string) (provider.WorkspaceSearch, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return provider.WorkspaceSearch{}, fmt.Errorf("%w: empty query", errWorkspaceInvalidQuery)
	}
	if len(q) > maxWorkspaceQueryLen {
		return provider.WorkspaceSearch{}, fmt.Errorf("%w: query exceeds %d bytes", errWorkspaceInvalidQuery, maxWorkspaceQueryLen)
	}
	if strings.ContainsRune(q, 0) || !utf8.ValidString(q) {
		return provider.WorkspaceSearch{}, fmt.Errorf("%w: query is not valid UTF-8 text", errWorkspaceInvalidQuery)
	}
	switch kind {
	case provider.WorkspaceSearchText:
		return o.searchText(ctx, q)
	case provider.WorkspaceSearchFile:
		return o.searchFiles(ctx, q)
	default:
		return provider.WorkspaceSearch{}, fmt.Errorf("%w: unknown search kind %q", errWorkspaceInvalidQuery, kind)
	}
}

func (o *httpSession) searchText(ctx context.Context, query string) (provider.WorkspaceSearch, error) {
	params := url.Values{}
	params.Set("pattern", query)
	var upstream []struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	}
	if err := o.workspaceGet(ctx, "/find", params, &upstream); err != nil {
		return provider.WorkspaceSearch{}, err
	}
	out := provider.WorkspaceSearch{Kind: provider.WorkspaceSearchText, Cap: textSearchCap}
	for _, u := range upstream {
		p, ok := validateReturnedPath(u.Path.Text)
		if !ok {
			continue
		}
		m := provider.WorkspaceMatch{
			Path: p,
			Line: u.LineNumber,
			Text: clipBlock(strings.TrimRight(u.Lines.Text, "\r\n"), 500),
		}
		if len(u.Submatches) > 0 {
			m.Column = u.Submatches[0].Start + 1
		}
		out.Matches = append(out.Matches, m)
	}
	sortMatches(out.Matches)
	// The engine's own hard cap is what bounds this, not the row budget.
	if len(out.Matches) >= textSearchCap {
		out.Truncated = true
	}
	if len(out.Matches) > maxWorkspaceMatches {
		out.Matches = out.Matches[:maxWorkspaceMatches]
		out.Truncated = true
	}
	return out, nil
}

func (o *httpSession) searchFiles(ctx context.Context, query string) (provider.WorkspaceSearch, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", fmt.Sprint(fileSearchLimit))
	var upstream []string
	if err := o.workspaceGet(ctx, "/find/file", params, &upstream); err != nil {
		return provider.WorkspaceSearch{}, err
	}
	out := provider.WorkspaceSearch{Kind: provider.WorkspaceSearchFile, Cap: fileSearchLimit}
	for _, raw := range upstream {
		p, ok := validateReturnedPath(raw)
		if !ok {
			continue
		}
		out.Matches = append(out.Matches, provider.WorkspaceMatch{Path: p})
	}
	sortMatches(out.Matches)
	if len(out.Matches) > maxWorkspaceMatches {
		out.Matches = out.Matches[:maxWorkspaceMatches]
		out.Truncated = true
	}
	return out, nil
}

// sortMatches orders by path, then line, then column, so two runs of the same
// search render identically regardless of engine order.
func sortMatches(m []provider.WorkspaceMatch) {
	sort.SliceStable(m, func(i, j int) bool {
		if m[i].Path != m[j].Path {
			return m[i].Path < m[j].Path
		}
		if m[i].Line != m[j].Line {
			return m[i].Line < m[j].Line
		}
		return m[i].Column < m[j].Column
	})
}
