package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

const fastTierName = "Fast"

type serviceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type modelRecord struct {
	ID                        string
	DisplayName               string
	Description               string
	Hidden                    bool
	IsDefault                 bool
	DefaultReasoningEffort    string
	InputModalities           []string
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
		Description     string `json:"description"`
	}
	ServiceTiers        []serviceTier
	DefaultServiceTier  string
	SupportsPersonality bool
}

func decodeModelListEntry(raw modelListEntry) (modelRecord, error) {
	tiers := make([]serviceTier, 0, len(raw.ServiceTiers))
	fastSeen := false
	for _, t := range raw.ServiceTiers {
		id := strings.TrimSpace(t.ID)
		if id == "" {
			return modelRecord{}, fmt.Errorf("model %q: empty service tier id", raw.ID)
		}
		if t.Name == fastTierName {
			if fastSeen {
				return modelRecord{}, fmt.Errorf("model %q: duplicate Fast service tier", raw.ID)
			}
			fastSeen = true
		}
		tiers = append(tiers, serviceTier{
			ID:          id,
			Name:        t.Name,
			Description: t.Description,
		})
	}
	return modelRecord{
		ID:                        raw.ID,
		DisplayName:               raw.DisplayName,
		Description:               raw.Description,
		Hidden:                    raw.Hidden,
		IsDefault:                 raw.IsDefault,
		DefaultReasoningEffort:    raw.DefaultReasoningEffort,
		InputModalities:           append([]string(nil), raw.InputModalities...),
		SupportedReasoningEfforts: raw.SupportedReasoningEfforts,
		ServiceTiers:              tiers,
		DefaultServiceTier:        strings.TrimSpace(raw.DefaultServiceTier),
		SupportsPersonality:       raw.SupportsPersonality,
	}, nil
}

func decodeModelRecords(raw []byte) ([]modelRecord, error) {
	var page modelListPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	out := make([]modelRecord, 0, len(page.Data))
	for _, e := range page.Data {
		rec, err := decodeModelListEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func resolveActiveModel(sessionModel, cfgModel, catalogDefault string) string {
	if s := strings.TrimSpace(sessionModel); s != "" {
		return s
	}
	if s := strings.TrimSpace(cfgModel); s != "" {
		return s
	}
	return strings.TrimSpace(catalogDefault)
}

func (r modelRecord) fastTier() (serviceTier, bool) {
	for _, t := range r.ServiceTiers {
		if t.Name == fastTierName {
			return t, true
		}
	}
	return serviceTier{}, false
}

func (r modelRecord) pickerOption() picker.Option {
	meta := map[string]string{}
	if r.DefaultReasoningEffort != "" {
		meta["reasoning_effort"] = r.DefaultReasoningEffort
	}
	if len(r.InputModalities) > 0 {
		meta["input"] = strings.Join(r.InputModalities, ",")
	}
	if len(meta) == 0 {
		meta = nil
	}
	levels := make([]picker.ThinkingLevel, 0, len(r.SupportedReasoningEfforts))
	for _, e := range r.SupportedReasoningEfforts {
		if e.ReasoningEffort == "" {
			continue
		}
		levels = append(levels, picker.ThinkingLevel{
			ID:          e.ReasoningEffort,
			Description: e.Description,
			Default:     e.ReasoningEffort == r.DefaultReasoningEffort,
		})
	}
	return picker.Option{
		ID:             r.ID,
		Label:          r.DisplayName,
		Description:    r.Description,
		Meta:           meta,
		ThinkingLevels: picker.NormalizeThinkingLevels(levels),
	}
}

func catalogDefaultID(recs []modelRecord, cfgModel string) string {
	if cfgModel != "" {
		return cfgModel
	}
	for _, r := range recs {
		if r.IsDefault && r.ID != "" && !r.Hidden {
			return r.ID
		}
	}
	return ""
}

func lookupModel(recs []modelRecord, id string) (modelRecord, bool) {
	for _, r := range recs {
		if r.ID == id {
			return r, true
		}
	}
	return modelRecord{}, false
}

func normalizeServiceTier(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.EqualFold(id, "default") {
		return ""
	}
	return id
}
