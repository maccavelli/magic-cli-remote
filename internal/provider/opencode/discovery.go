package opencode

// Provider-native discovery: resumable root sessions and engine-known projects
// (MADR 0112 A1).
//
// Both are read-only metadata surfaces. Nothing here imports a transcript or
// creates a session: the phone shows what the engine already has, the user
// picks one, and the ordinary session-create path runs afterwards with the
// selected ID or directory. That separation is what keeps discovery from
// becoming a second, weaker way to start work.

import (
	"context"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Bounds on provider-controlled discovery data. The engine is trusted to be
// non-malicious but not to be small: a long-lived workstation accumulates
// thousands of sessions, and every one of these fields is rendered on a phone.
const (
	maxListedSessions = 100
	maxListedProjects = 100

	maxSessionIDLen  = 256
	maxSessionCWDLen = 4096

	maxProjectIDLen       = 256
	maxProjectNameLen     = 128
	maxProjectWorktreeLen = 4096

	maxModelIDLen = 256
	maxAgentLen   = 128

	discoveryTimeout = 30 * time.Second
)

// globalProjectID is the project OpenCode registers for a directory that is not
// in a version-control worktree. Its worktree resolves to the filesystem root.
//
// A fresh engine does not list it; it appears the moment any non-Git directory
// is opened and then persists, so any engine a user has actually driven will
// have one. Offering "/" as a selectable project root would invite a session
// whose working directory is the whole machine, so it is filtered on both the
// id and the worktree — either alone would be a weaker check than the pair.
const globalProjectID = "global"

// wireSession is the subset of OpenCode's Session schema discovery reads.
//
// Every field below is optional in practice even where the schema marks it
// required, because a session that never ran a model turn returns explicit
// nulls for model, agent, parentID and share. Pointer and omitted-value
// handling here is not defensive padding: it is the observed 1.18.21 shape.
type wireSession struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parentID"`
	Directory string  `json:"directory"`
	Title     string  `json:"title"`
	Agent     *string `json:"agent"`
	Model     *struct {
		ProviderID string `json:"providerID"`
		ID         string `json:"id"`
		Variant    string `json:"variant"`
	} `json:"model"`
	Cost   *float64 `json:"cost"`
	Tokens *struct {
		Input     float64 `json:"input"`
		Output    float64 `json:"output"`
		Reasoning float64 `json:"reasoning"`
		Cache     struct {
			Read  float64 `json:"read"`
			Write float64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// wireProject is the subset of OpenCode's Project schema discovery reads.
type wireProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Worktree string `json:"worktree"`
}

// ListAgentSessionsLive implements [httpagent.AgentSessionDiscoverer].
//
// `roots=true` asks the engine for top-level sessions only. The result is
// re-filtered on parentID anyway: a child session shown as resumable would let
// the phone attach to a subagent as though it were a conversation, and
// depending on a query parameter alone to prevent that is a single point of
// failure for a correctness property.
func (d *httpDialect) ListAgentSessionsLive(ctx context.Context, api httpagent.API) ([]provider.AgentSessionMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	var raw []wireSession
	if err := api(ctx, "GET", "/session?roots=true&limit=100", nil, &raw); err != nil {
		return nil, err
	}

	out := make([]provider.AgentSessionMeta, 0, min(len(raw), maxListedSessions))
	for _, s := range raw {
		id := clip(strings.TrimSpace(s.ID), maxSessionIDLen)
		if id == "" {
			continue
		}
		if s.ParentID != nil && strings.TrimSpace(*s.ParentID) != "" {
			continue // a child session is not a resumable root
		}
		meta := provider.AgentSessionMeta{
			ID:    id,
			CWD:   clip(strings.TrimSpace(s.Directory), maxSessionCWDLen),
			Title: clip(strings.TrimSpace(s.Title), maxSessionTitleLen),
		}
		if s.Time.Updated > 0 {
			meta.UpdatedAt = time.UnixMilli(s.Time.Updated).UTC()
		} else if s.Time.Created > 0 {
			meta.UpdatedAt = time.UnixMilli(s.Time.Created).UTC()
		}
		if s.Agent != nil {
			meta.Agent = clip(strings.TrimSpace(*s.Agent), maxAgentLen)
		}
		if s.Model != nil {
			p := strings.TrimSpace(s.Model.ProviderID)
			m := strings.TrimSpace(s.Model.ID)
			if p != "" && m != "" {
				meta.ModelID = clip(p+"/"+m, maxModelIDLen)
			}
			// "default" is OpenCode's sentinel for "no variant override", so it
			// is not a rung and must not be shown as one (MADR 0112 A14).
			if v := strings.TrimSpace(s.Model.Variant); v != "" && v != "default" {
				meta.ThinkingLevel = clip(v, maxAgentLen)
			}
		}
		meta.Aggregate = sessionUsage(s)
		out = append(out, meta)
	}

	// Newest first, with the ID as a tie-break so a page of sessions updated in
	// the same millisecond — which batch operations do produce — has a stable
	// order across refreshes rather than shuffling under the user's finger.
	slices.SortFunc(out, func(a, b provider.AgentSessionMeta) int {
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			if a.UpdatedAt.After(b.UpdatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(out) > maxListedSessions {
		out = out[:maxListedSessions]
	}
	return out, nil
}

// sessionUsage converts OpenCode's whole-session accounting, or returns nil
// when the engine reported none.
//
// The wire types are JSON numbers, so a malformed or hostile payload can carry
// a negative, fractional, NaN or infinite value. Each is dropped rather than
// coerced: a negative token count rendered as a huge unsigned number, or a NaN
// cost serialized into JSON, is worse than an absent field.
func sessionUsage(s wireSession) *provider.AgentSessionUsage {
	if s.Tokens == nil && s.Cost == nil {
		return nil
	}
	u := &provider.AgentSessionUsage{}
	if s.Tokens != nil {
		u.Input = tokenCount(s.Tokens.Input)
		u.Output = tokenCount(s.Tokens.Output)
		u.Reasoning = tokenCount(s.Tokens.Reasoning)
		u.CacheRead = tokenCount(s.Tokens.Cache.Read)
		u.CacheWrite = tokenCount(s.Tokens.Cache.Write)
	}
	if s.Cost != nil {
		c := *s.Cost
		if !math.IsNaN(c) && !math.IsInf(c, 0) && c >= 0 {
			u.CostUSD = &c
		}
	}
	return u
}

// tokenCount clamps one JSON number to a non-negative whole token count.
func tokenCount(v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	return int64(v)
}

// ListProjectsLive implements [httpagent.ProjectDiscoverer].
func (d *httpDialect) ListProjectsLive(ctx context.Context, api httpagent.API) ([]provider.ProjectMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	var raw []wireProject
	if err := api(ctx, "GET", "/project", nil, &raw); err != nil {
		return nil, err
	}

	out := make([]provider.ProjectMeta, 0, min(len(raw), maxListedProjects))
	for _, p := range raw {
		id := clip(strings.TrimSpace(p.ID), maxProjectIDLen)
		worktree := strings.TrimSpace(p.Worktree)
		if id == "" || worktree == "" {
			continue
		}
		// A relative worktree cannot be validated against the session CWD rules
		// the client applies next, and the root sentinel must never be
		// selectable.
		if !strings.HasPrefix(worktree, "/") || len(worktree) > maxProjectWorktreeLen {
			continue
		}
		// The hazard is the *worktree*, not the id: a session rooted at "/" has
		// the whole machine as its working directory. Reject that outright, so
		// an engine that ever registers the filesystem root under some other id
		// cannot slip through. The id check is kept as a named, documented case
		// because the sentinel is the way this actually occurs.
		if path.Clean(worktree) == "/" {
			if id != globalProjectID {
				d.log.Debug("dropping a project rooted at the filesystem root",
					"project_id", id)
			}
			continue
		}
		name := clip(strings.TrimSpace(p.Name), maxProjectNameLen)
		if name == "" {
			// Base name only. The full path is exactly what the phone is trying
			// not to render, and it is already carried in Worktree.
			name = clip(path.Base(path.Clean(worktree)), maxProjectNameLen)
		}
		out = append(out, provider.ProjectMeta{ID: id, Name: name, Worktree: worktree})
	}

	slices.SortFunc(out, func(a, b provider.ProjectMeta) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(out) > maxListedProjects {
		out = out[:maxListedProjects]
	}
	return out, nil
}

// Compile-time proof that the dialect satisfies both discovery interfaces, so
// a signature drift fails the build instead of silently making the provider
// report discovery as unsupported at runtime.
var (
	_ httpagent.AgentSessionDiscoverer = (*httpDialect)(nil)
	_ httpagent.ProjectDiscoverer      = (*httpDialect)(nil)
)
