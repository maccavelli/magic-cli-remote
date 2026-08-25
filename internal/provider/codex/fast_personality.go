package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var validPersonalities = map[string]struct{}{
	"friendly":  {},
	"pragmatic": {},
	"none":      {},
}

func (s *session) activeModelRecord() (modelRecord, bool) {
	var recs []modelRecord
	cfgModel := ""
	if s.p != nil {
		recs = s.p.cachedModels()
		cfgModel = s.p.cfg.Model
	}
	id := resolveActiveModel(s.opts.Model, cfgModel, catalogDefaultID(recs, cfgModel))
	return lookupModel(recs, id)
}

func (s *session) HasFast() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.activeModelRecord()
	if !ok {
		return false
	}
	_, ok = rec.fastTier()
	return ok
}

func (s *session) ServiceTier() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serviceTier
}

func (s *session) SetServiceTier(ctx context.Context, on bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	rec, ok := s.activeModelRecord()
	if !ok {
		s.mu.Unlock()
		return provider.ErrServiceTierUnsupported
	}
	fast, ok := rec.fastTier()
	if !ok {
		s.mu.Unlock()
		return provider.ErrServiceTierUnsupported
	}
	next := ""
	if on {
		next = fast.ID
	}
	if s.serviceTier == next {
		s.mu.Unlock()
		return nil
	}
	gen := s.engineGeneration
	threadID := s.agentID
	experimental := s.p != nil && s.p.supportsCapability(CapabilityThreadSettings)
	s.mu.Unlock()
	return s.applySetting(ctx, gen, threadID, experimental, func(params map[string]any) {
		if next == "" {
			params["serviceTier"] = nil
		} else {
			params["serviceTier"] = next
		}
	}, func() {
		s.serviceTier = next
	})
}

func (s *session) PersonalitySupported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.activeModelRecord()
	return ok && rec.SupportsPersonality
}

func (s *session) Personality() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.personality
}

func (s *session) SetPersonality(ctx context.Context, value string) error {
	value = strings.TrimSpace(strings.ToLower(value))
	if _, ok := validPersonalities[value]; !ok {
		return provider.ErrPersonalityInvalid
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	rec, ok := s.activeModelRecord()
	if !ok || !rec.SupportsPersonality {
		s.mu.Unlock()
		return provider.ErrPersonalityUnsupported
	}
	if s.personality == value {
		s.mu.Unlock()
		return nil
	}
	gen := s.engineGeneration
	threadID := s.agentID
	experimental := s.p != nil && s.p.supportsCapability(CapabilityThreadSettings)
	s.mu.Unlock()
	return s.applySetting(ctx, gen, threadID, experimental, func(params map[string]any) {
		params["personality"] = value
	}, func() {
		s.personality = value
	})
}

func (s *session) applySetting(ctx context.Context, gen int, threadID string, experimental bool, fill func(map[string]any), commit func()) error {
	if !experimental {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed || s.engineGeneration != gen {
			return nil
		}
		commit()
		return provider.ErrAppliesNextTurn
	}
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	params := map[string]any{"threadId": threadID}
	fill(params)
	if _, err := fr.sendRequest(ctx, "thread/settings/update", params); err != nil {
		var rpc *rpcErrorBody
		if errors.As(err, &rpc) && rpc != nil && (rpc.IsMethodNotFound() || rpc.IsInvalidParams()) {
			reason := DenialMethodNotFound
			if rpc.IsInvalidParams() {
				reason = DenialInvalidParams
			}
			s.p.disableCapability(CapabilityThreadSettings, reason)
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.closed && s.engineGeneration == gen {
				commit()
			}
			return provider.ErrAppliesNextTurn
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.engineGeneration != gen {
		return nil
	}
	commit()
	return nil
}

func (s *session) revalidateModelSettingsLocked() {
	rec, ok := s.activeModelRecord()
	if !ok {
		s.serviceTier = ""
		s.personality = ""
		return
	}
	if _, hasFast := rec.fastTier(); !hasFast {
		s.serviceTier = ""
	}
	if !rec.SupportsPersonality {
		s.personality = ""
	}
}

func applyServiceTurnParams(params map[string]any, tier, personality string) {
	if normalizeServiceTier(tier) == "" {
		params["serviceTier"] = nil
	} else {
		params["serviceTier"] = tier
	}
	if personality != "" {
		params["personality"] = personality
	}
}

func applySettingsServiceFields(s *session, raw json.RawMessage) {
	var parsed struct {
		ThreadSettings struct {
			ServiceTier json.RawMessage `json:"serviceTier"`
			Personality *string         `json:"personality"`
		} `json:"threadSettings"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(parsed.ThreadSettings.ServiceTier) > 0 && string(parsed.ThreadSettings.ServiceTier) != "null" {
		var id string
		if json.Unmarshal(parsed.ThreadSettings.ServiceTier, &id) == nil {
			s.serviceTier = normalizeServiceTier(id)
		}
	} else if string(parsed.ThreadSettings.ServiceTier) == "null" {
		s.serviceTier = ""
	}
	if parsed.ThreadSettings.Personality != nil {
		p := strings.TrimSpace(*parsed.ThreadSettings.Personality)
		if _, ok := validPersonalities[p]; ok {
			s.personality = p
		}
	}
}

var _ provider.ServiceTierSession = (*session)(nil)
var _ provider.PersonalitySession = (*session)(nil)
