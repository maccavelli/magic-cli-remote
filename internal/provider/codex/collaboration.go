package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	collaborationModePlan    = "plan"
	collaborationModeDefault = "default"
	reasonCatalogInvalid     = "collaboration catalog is invalid"
)

var experimentalCapabilityRe = regexp.MustCompile(`(?i)experimental`)

func reasonExperimentalUnavailable(version string) string {
	if version == "" {
		version = "unknown"
	}
	return "codex " + version + " does not expose collaboration modes (experimental API unavailable)"
}

type collaborationModeMask struct {
	Name            string
	Mode            string
	Model           *string
	ReasoningEffort *string
}

type collaborationCatalog struct {
	modes []collaborationModeMask
}

func (c collaborationCatalog) has(id string) bool {
	_, ok := c.lookup(id)
	return ok
}

func (c collaborationCatalog) lookup(id string) (collaborationModeMask, bool) {
	id = strings.TrimSpace(id)
	for _, m := range c.modes {
		if strings.EqualFold(m.Mode, id) {
			return m, true
		}
	}
	return collaborationModeMask{}, false
}

type collaborationProbe struct {
	probed    bool
	supported bool
	reason    string
	catalog   collaborationCatalog
}

func isExperimentalInitRejection(err error) bool {
	var rpc *rpcErrorBody
	if !errors.As(err, &rpc) || rpc == nil {
		return false
	}
	if experimentalCapabilityRe.MatchString(rpc.Message) {
		return true
	}
	return len(rpc.Data) > 0 && experimentalCapabilityRe.MatchString(string(rpc.Data))
}

func decodeCollaborationCatalog(raw []byte) (collaborationCatalog, error) {
	var parsed struct {
		Data []struct {
			Name            string          `json:"name"`
			Mode            *string         `json:"mode"`
			Model           *string         `json:"model"`
			ReasoningEffort json.RawMessage `json:"reasoning_effort"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return collaborationCatalog{}, err
	}
	seen := make(map[string]struct{}, len(parsed.Data))
	out := make([]collaborationModeMask, 0, len(parsed.Data))
	for _, row := range parsed.Data {
		if row.Mode == nil || strings.TrimSpace(*row.Mode) == "" {
			return collaborationCatalog{}, fmt.Errorf("%s: empty mode id", reasonCatalogInvalid)
		}
		id := strings.TrimSpace(*row.Mode)
		if _, dup := seen[id]; dup {
			return collaborationCatalog{}, fmt.Errorf("%s: duplicate mode %q", reasonCatalogInvalid, id)
		}
		seen[id] = struct{}{}
		effort, err := decodeReasoningEffort(row.ReasoningEffort)
		if err != nil {
			return collaborationCatalog{}, err
		}
		out = append(out, collaborationModeMask{
			Name:            row.Name,
			Mode:            id,
			Model:           row.Model,
			ReasoningEffort: effort,
		})
	}
	cat := collaborationCatalog{modes: out}
	if !cat.has(collaborationModePlan) || !cat.has(collaborationModeDefault) {
		return collaborationCatalog{}, fmt.Errorf("%s: missing plan or default", reasonCatalogInvalid)
	}
	return cat, nil
}

func decodeReasoningEffort(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: invalid reasoning effort", reasonCatalogInvalid)
	}
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	return &s, nil
}

func (p *Provider) versionLabel() string {
	if p.version != "" {
		return p.version
	}
	return "unknown"
}

func (p *Provider) collaborationCapability() (ok bool, reason string, catalog collaborationCatalog, gen int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return false, reasonExperimentalUnavailable(p.versionLabel()), collaborationCatalog{}, 0
	}
	return p.eng.collab.supported, p.eng.collab.reason, p.eng.collab.catalog, p.eng.generation
}

func (p *Provider) probeCollaboration(ctx context.Context, eng *engine) {
	if eng == nil || eng.conn == nil {
		return
	}
	p.mu.Lock()
	if eng.collab.probed {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	raw, err := eng.conn.sendRequest(ctx, "collaborationMode/list", map[string]any{})
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng != eng || eng.collab.probed {
		return
	}
	eng.collab.probed = true
	if err != nil {
		eng.collab.supported = false
		eng.collab.catalog = collaborationCatalog{}
		eng.collab.reason = reasonExperimentalUnavailable(p.versionLabel())
		p.log.Info("codex collaboration unavailable",
			slog.String("method", "collaborationMode/list"),
			slog.String("codex_version", p.versionLabel()),
			slog.Int("generation", eng.generation),
			slog.String("reason", eng.collab.reason),
		)
		return
	}
	cat, decErr := decodeCollaborationCatalog(raw)
	if decErr != nil {
		eng.collab.supported = false
		eng.collab.catalog = collaborationCatalog{}
		eng.collab.reason = reasonCatalogInvalid
		p.log.Info("codex collaboration catalog invalid",
			slog.String("method", "collaborationMode/list"),
			slog.String("codex_version", p.versionLabel()),
			slog.Int("generation", eng.generation),
			slog.String("reason", eng.collab.reason),
		)
		return
	}
	eng.collab.supported = true
	eng.collab.catalog = cat
	eng.collab.reason = ""
}

func (s *session) seedCollaboration() {
	if s.p == nil {
		return
	}
	ok, _, cat, gen := s.p.collaborationCapability()
	s.collabSupported = ok
	s.collabCatalog = cat
	s.engineGeneration = gen
	want := strings.TrimSpace(s.opts.CollaborationModeID)
	if want == "" || !cat.has(want) {
		want = collaborationModeDefault
	}
	if !ok {
		want = collaborationModeDefault
	}
	s.collabMode = want
}

func (s *session) effectiveModelLocked() string {
	if s.opts.Model != "" {
		return s.opts.Model
	}
	return s.cfg.Model
}

func buildCollaborationSettings(mask collaborationModeMask, model, userEffort, mode string) map[string]any {
	var effort any
	if mode == collaborationModePlan {
		if mask.ReasoningEffort != nil {
			effort = *mask.ReasoningEffort
		} else {
			effort = nil
		}
	} else if userEffort != "" {
		effort = userEffort
	} else {
		effort = nil
	}
	return map[string]any{
		"mode": mode,
		"settings": map[string]any{
			"model":                  model,
			"reasoning_effort":       effort,
			"developer_instructions": nil,
		},
	}
}

func (s *session) CollaborationModes() ([]provider.CollaborationMode, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.collabSupported {
		return nil, "", provider.ErrCollaborationUnsupported
	}
	out := make([]provider.CollaborationMode, 0, len(s.collabCatalog.modes))
	for _, m := range s.collabCatalog.modes {
		name := m.Name
		if name == "" {
			name = m.Mode
		}
		out = append(out, provider.CollaborationMode{ID: m.Mode, Name: name})
	}
	return out, s.collabMode, nil
}

func (s *session) SetCollaborationMode(ctx context.Context, modeID string) error {
	modeID = strings.TrimSpace(modeID)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnBusy {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	if !s.collabSupported {
		s.mu.Unlock()
		return provider.ErrCollaborationUnsupported
	}
	mask, ok := s.collabCatalog.lookup(modeID)
	if !ok {
		s.mu.Unlock()
		return provider.ErrCollaborationInvalid
	}
	if strings.EqualFold(s.collabMode, modeID) {
		s.mu.Unlock()
		return nil
	}
	model := s.effectiveModelLocked()
	effort := s.thinkingLevel
	gen := s.engineGeneration
	threadID := s.agentID
	s.mu.Unlock()

	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	_, err := fr.sendRequest(ctx, "thread/settings/update", map[string]any{
		"threadId":          threadID,
		"collaborationMode": buildCollaborationSettings(mask, model, effort, modeID),
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.engineGeneration != gen {
		return nil
	}
	s.collabMode = modeID
	return nil
}

func (s *session) applySettingsUpdated(params json.RawMessage) {
	var parsed struct {
		ThreadSettings struct {
			CollaborationMode *struct {
				Mode string `json:"mode"`
			} `json:"collaborationMode"`
		} `json:"threadSettings"`
	}
	if err := json.Unmarshal(params, &parsed); err != nil {
		return
	}
	if parsed.ThreadSettings.CollaborationMode == nil {
		return
	}
	mode := strings.TrimSpace(parsed.ThreadSettings.CollaborationMode.Mode)
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "" || !s.collabCatalog.has(mode) {
		return
	}
	s.collabMode = mode
}

func applyCollaborationTurnParams(params map[string]any, supported bool, mask collaborationModeMask, mode, model, userEffort string) {
	if !supported || mode == "" || model == "" {
		if userEffort != "" {
			params["effort"] = userEffort
		}
		return
	}
	params["collaborationMode"] = buildCollaborationSettings(mask, model, userEffort, mode)
	if mode != collaborationModePlan && userEffort != "" {
		params["effort"] = userEffort
	}
}

var _ provider.CollaborationModeSession = (*session)(nil)
