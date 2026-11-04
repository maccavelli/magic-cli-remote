package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// The workspace, diagnostics and diff of an ACP session, served by the control
// plane in the session's directory (0214 MADR D10, as amended at P6d):
//   - workspace on fs.list, fs.read and fs.find, behind the 1.x path guards
//     (workspace.go), which run before any request and again on every path
//     the engine returns: given an absolute path or "..", fs.list lists the
//     directory above the project;
//   - fs.read answers the file's bytes, so it is a raw request, kept out of
//     the wire capture;
//   - full-text search has no v2 route, and says so (#34);
//   - diagnostics from vcs, vcs/status and skill. MCP state is not read: the
//     control plane is a second opencode process with its own copies of the
//     user's MCP servers, so its states are not the engine's (PLAN P6d
//     Deviation 1). LSP and formatter status have no v2 route;
//   - the diff of the whole session, from its first user message to its
//     newest: with no `from`, session.diff diffs only the newest turn.
//
// No answer's error text reaches the phone: an engine failure is reported by
// route, status and tag, because its message can carry a host path.

const (
	// acpSurfaceTimeout bounds one workspace or diff operation: a few local
	// calls once the control plane runs.
	acpSurfaceTimeout = 30 * time.Second
	// acpDiagnosticsTimeout bounds the diagnostics report, which may wait for
	// plugin activation in a location the control plane has not used.
	acpDiagnosticsTimeout = 45 * time.Second
	// acpDiffMaxFiles is how many files the diff summary lists, as on 1.x.
	acpDiffMaxFiles = 20
)

// errACPTextSearch is full-text search's answer: v2 has no route for it (0214
// MADR, settled 2026-11-01).
var errACPTextSearch = errors.New("full-text search is not available on OpenCode v2 yet (issue #34); search by file name instead")

// acpSurfaces serves the Spec's workspace, diagnostics and diff hooks.
type acpSurfaces struct {
	plane controlPlane
}

// get sends one JSON request in the session's directory and decodes a 2xx
// answer into out, refusing one larger than the workspace envelope.
func (s *acpSurfaces) get(ctx context.Context, h acpagent.Host, route string, q url.Values, out any) error {
	res, err := s.plane.Do(ctx, control.Request{Method: http.MethodGet, Path: route, Query: q, Directory: h.CWD()})
	if err != nil {
		return err
	}
	if err := answerOK(route, res); err != nil {
		return err
	}
	if len(res.Body) > maxWorkspaceEnvelope {
		return fmt.Errorf("%w: response exceeds %d bytes", errWorkspaceTooLarge, maxWorkspaceEnvelope)
	}
	if out == nil || len(res.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return fmt.Errorf("opencode %s: the answer could not be decoded", route)
	}
	return nil
}

// answerOK turns an answer outside 2xx into an error naming the route, the
// status and the engine's tag, never its message. A FileNotFoundError is
// invalid_path, which the phone shows in words of its own.
func answerOK(route string, res control.Response) error {
	if res.Status >= 200 && res.Status <= 299 {
		return nil
	}
	var b struct {
		Tag string `json:"_tag"`
	}
	_ = json.Unmarshal(res.Body, &b)
	if b.Tag == "FileNotFoundError" {
		return fmt.Errorf("%w: no such file or directory", errWorkspaceInvalidPath)
	}
	return &engineError{method: http.MethodGet, path: route, status: res.Status, tag: b.Tag}
}

// activate waits for plugin activation in the session's location: the
// integration list is the route that waits for it, and a skill list read
// before it answers only some skills (PLAN P6d Deviation 1). Like acpAuth.list
// it runs apart from ctx, so a cold start that outlasts the caller still
// finishes and serves the next one.
func (s *acpSurfaces) activate(ctx context.Context, h acpagent.Host) error {
	dir := h.CWD()
	done := make(chan error, 1)
	go func() {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpListBound)
		defer cancel()
		res, err := s.plane.Do(lctx, control.Request{Method: http.MethodGet, Path: "/api/integration", Directory: dir})
		if err == nil {
			err = answerOK("/api/integration", res)
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// workspacePath is the 1.x pre-dispatch check, against the session's root.
func workspacePath(h acpagent.Host, in string) (string, error) {
	rel, err := normalizeWorkspacePath(in)
	if err != nil {
		return "", err
	}
	if err := checkNoSymlinkComponents(h.CWD(), rel); err != nil {
		return "", err
	}
	return rel, nil
}

// fsEntry is one fs.list or fs.find row: a path relative to the location,
// ending in "/" for a directory.
type fsEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (s *acpSurfaces) listWorkspace(ctx context.Context, h acpagent.Host, reqPath string) ([]provider.WorkspaceEntry, error) {
	rel, err := workspacePath(h, reqPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, acpSurfaceTimeout)
	defer cancel()
	q := url.Values{"path": {"."}}
	if rel != "" {
		q.Set("path", rel)
	}
	var out struct {
		Data []fsEntry `json:"data"`
	}
	if err := s.get(ctx, h, "/api/fs/list", q, &out); err != nil {
		return nil, err
	}
	entries := make([]provider.WorkspaceEntry, 0, len(out.Data))
	for _, e := range out.Data {
		p, ok := validateReturnedPath(e.Path)
		if !ok {
			continue
		}
		entries = append(entries, provider.WorkspaceEntry{
			Name: path.Base(p),
			Path: p,
			Dir:  e.Type == "directory" || strings.HasSuffix(e.Path, "/"),
		})
	}
	// Directories first, then by path, as on 1.x.
	slices.SortStableFunc(entries, func(a, b provider.WorkspaceEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	if len(entries) > maxWorkspaceEntries {
		entries = entries[:maxWorkspaceEntries]
	}
	return entries, nil
}

func (s *acpSurfaces) readWorkspace(ctx context.Context, h acpagent.Host, reqPath string) (provider.WorkspaceContent, error) {
	rel, err := workspacePath(h, reqPath)
	if err != nil {
		return provider.WorkspaceContent{}, err
	}
	if rel == "" {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: no file named", errWorkspaceInvalidPath)
	}
	ctx, cancel := context.WithTimeout(ctx, acpSurfaceTimeout)
	defer cancel()
	route := "/api/fs/read/" + escapeSegments(rel)
	res, err := s.plane.Do(ctx, control.Request{Method: http.MethodGet, Path: route, Directory: h.CWD(),
		Raw: true, MaxBytes: maxWorkspaceFileBytes})
	if err != nil {
		return provider.WorkspaceContent{}, err
	}
	if err := answerOK("/api/fs/read", res); err != nil {
		return provider.WorkspaceContent{}, err
	}
	// As on 1.x: a file too large is refused, never shown in part, and bytes
	// that are not text are refused rather than shown garbled.
	if res.Truncated {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: file exceeds %d bytes", errWorkspaceTooLarge, maxWorkspaceFileBytes)
	}
	text := string(res.Body)
	if strings.ContainsRune(text, 0) {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: content contains NUL", errWorkspaceBinary)
	}
	if !utf8.ValidString(text) {
		return provider.WorkspaceContent{}, fmt.Errorf("%w: content is not valid UTF-8", errWorkspaceBinary)
	}
	return provider.WorkspaceContent{Path: rel, Text: text, Bytes: len(text)}, nil
}

// escapeSegments escapes each segment of a cleaned relative path for a URL
// path, keeping the separators.
func escapeSegments(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (s *acpSurfaces) searchWorkspace(ctx context.Context, h acpagent.Host, kind, query string) (provider.WorkspaceSearch, error) {
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
		return provider.WorkspaceSearch{}, errACPTextSearch
	case provider.WorkspaceSearchFile:
	default:
		return provider.WorkspaceSearch{}, fmt.Errorf("%w: unknown search kind %q", errWorkspaceInvalidQuery, kind)
	}
	ctx, cancel := context.WithTimeout(ctx, acpSurfaceTimeout)
	defer cancel()
	var out struct {
		Data []fsEntry `json:"data"`
	}
	params := url.Values{"query": {q}, "limit": {fmt.Sprint(fileSearchLimit)}}
	if err := s.get(ctx, h, "/api/fs/find", params, &out); err != nil {
		return provider.WorkspaceSearch{}, err
	}
	res := provider.WorkspaceSearch{Kind: provider.WorkspaceSearchFile, Cap: fileSearchLimit}
	for _, e := range out.Data {
		// A match is a file to open; a directory is browsed, not read.
		if e.Type == "directory" || strings.HasSuffix(e.Path, "/") {
			continue
		}
		if p, ok := validateReturnedPath(e.Path); ok {
			res.Matches = append(res.Matches, provider.WorkspaceMatch{Path: p})
		}
	}
	sortMatches(res.Matches)
	// The engine stops at the limit it was given, so a full page may hide more.
	if len(out.Data) >= fileSearchLimit {
		res.Truncated = true
	}
	if len(res.Matches) > maxWorkspaceMatches {
		res.Matches = res.Matches[:maxWorkspaceMatches]
		res.Truncated = true
	}
	return res, nil
}

func (s *acpSurfaces) diagnostics(ctx context.Context, h acpagent.Host) (provider.Diagnostics, error) {
	ctx, cancel := context.WithTimeout(ctx, acpDiagnosticsTimeout)
	defer cancel()
	var out provider.Diagnostics
	var successes int
	var vcs struct {
		Data struct {
			Branch struct {
				Current string `json:"current"`
				Default string `json:"default"`
			} `json:"branch"`
		} `json:"data"`
	}
	if err := s.get(ctx, h, "/api/vcs", nil, &vcs); err != nil {
		h.Log().Debug("opencode diagnostics vcs failed", "err", err)
	} else {
		out.Branch = clip(strings.TrimSpace(vcs.Data.Branch.Current), maxDiagnosticTextLen)
		out.DefaultBranch = clip(strings.TrimSpace(vcs.Data.Branch.Default), maxDiagnosticTextLen)
		successes++
	}
	// File names are counted, never copied: the summary is aggregate only.
	var status struct {
		Data []struct {
			Status    string `json:"status"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"data"`
	}
	if err := s.get(ctx, h, "/api/vcs/status", nil, &status); err != nil {
		h.Log().Debug("opencode diagnostics vcs status failed", "err", err)
	} else {
		summary := &provider.VCSStatusSummary{}
		for _, f := range status.Data {
			summary.Additions += max(f.Additions, 0)
			summary.Deletions += max(f.Deletions, 0)
			switch strings.ToLower(f.Status) {
			case "added":
				summary.Added++
			case "modified":
				summary.Modified++
			case "deleted":
				summary.Deleted++
			}
		}
		out.VCS = summary
		successes++
	}
	if skills, ok := s.skills(ctx, h); ok {
		out.Skills = skills
		successes++
	}
	if successes == 0 {
		return provider.Diagnostics{}, fmt.Errorf("opencode diagnostics unavailable")
	}
	return out, nil
}

// skills reads the location's skills after plugin activation, keeping name
// and description: a skill's path and its content are never decoded.
func (s *acpSurfaces) skills(ctx context.Context, h acpagent.Host) ([]provider.SkillInfo, bool) {
	if err := s.activate(ctx, h); err != nil {
		h.Log().Debug("opencode diagnostics: plugins not active; skills left out", "err", err)
		return nil, false
	}
	var upstream struct {
		Data []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := s.get(ctx, h, "/api/skill", nil, &upstream); err != nil {
		h.Log().Debug("opencode diagnostics skills failed", "err", err)
		return nil, false
	}
	out := make([]provider.SkillInfo, 0, len(upstream.Data))
	for _, u := range upstream.Data {
		name := clip(strings.TrimSpace(u.Name), maxSkillNameLen)
		if name == "" {
			continue
		}
		out = append(out, provider.SkillInfo{
			Name:        name,
			Description: clip(strings.TrimSpace(u.Description), maxSkillDescriptionLen),
		})
	}
	slices.SortFunc(out, func(a, b provider.SkillInfo) int { return strings.Compare(a.Name, b.Name) })
	if len(out) > maxDiagnosticSkills {
		out = out[:maxDiagnosticSkills]
	}
	return out, true
}

func (s *acpSurfaces) diff(ctx context.Context, h acpagent.Host, messageID string) (provider.DiffResult, error) {
	sid := h.AgentSessionID()
	if sid == "" {
		return provider.DiffResult{}, fmt.Errorf("opencode diff: the session has not started")
	}
	ctx, cancel := context.WithTimeout(ctx, acpSurfaceTimeout)
	defer cancel()
	q := url.Values{}
	if messageID != "" {
		q.Set("from", messageID)
	} else {
		first, err := s.userMessage(ctx, h, sid, "asc")
		if err != nil {
			return provider.DiffResult{}, err
		}
		if first == "" {
			return provider.DiffResult{Summary: "No file changes"}, nil
		}
		last, err := s.userMessage(ctx, h, sid, "desc")
		if err != nil {
			return provider.DiffResult{}, err
		}
		q.Set("from", first)
		if last != "" && last != first {
			q.Set("to", last)
		}
	}
	var out struct {
		Data []struct {
			File      string `json:"file"`
			Status    string `json:"status"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"data"`
	}
	if err := s.get(ctx, h, "/api/session/"+url.PathEscape(sid)+"/diff", q, &out); err != nil {
		return provider.DiffResult{}, err
	}
	if len(out.Data) == 0 {
		return provider.DiffResult{Summary: "No file changes"}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diff · %d file(s)\n", len(out.Data))
	for i, d := range out.Data {
		if i >= acpDiffMaxFiles {
			fmt.Fprintf(&b, "…and %d more\n", len(out.Data)-acpDiffMaxFiles)
			break
		}
		st := d.Status
		if st == "" {
			st = "modified"
		}
		fmt.Fprintf(&b, "  %s  +%d −%d  %s\n", st, d.Additions, d.Deletions, firstNonEmpty(d.File, "?"))
	}
	return provider.DiffResult{
		Summary:   strings.TrimRight(b.String(), "\n"),
		Truncated: len(out.Data) > acpDiffMaxFiles,
	}, nil
}

// userMessage is the id of the session's first (asc) or newest (desc) user
// message, or "" when it has none.
func (s *acpSurfaces) userMessage(ctx context.Context, h acpagent.Host, sid, order string) (string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	q := url.Values{"type": {"user"}, "order": {order}, "limit": {"1"}}
	if err := s.get(ctx, h, "/api/session/"+url.PathEscape(sid)+"/message", q, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", nil
	}
	return out.Data[0].ID, nil
}
