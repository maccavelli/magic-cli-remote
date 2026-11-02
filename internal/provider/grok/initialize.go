package grok

import (
	"encoding/json"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// grok's initialize `_meta`: its engine version, model and catalog.

// availableModel represents an available model in grok's initialize response.
type availableModel struct {
	ModelID string `json:"modelId"`
	Name    string `json:"name"`
	Meta    struct {
		TotalContextTokens      int    `json:"totalContextTokens"`
		SupportsReasoningEffort bool   `json:"supportsReasoningEffort"`
		ReasoningEffort         string `json:"reasoningEffort"`
		// ReasoningEfforts is the model's own ladder. Present only when
		// SupportsReasoningEffort is true; measured on grok 0.2.114 as
		// high (default), medium, low — expensive-first (MADR 0052 §2.2).
		ReasoningEfforts []struct {
			ID          string `json:"id"`
			Value       string `json:"value"`
			Label       string `json:"label"`
			Description string `json:"description"`
			Default     bool   `json:"default"`
		} `json:"reasoningEfforts"`
	} `json:"_meta"`
}

type grokInitializeMeta struct {
	Meta struct {
		DefaultAuthMethodID string `json:"defaultAuthMethodId"`
		// AgentVersion is grok's engine version. It is a vendor extension:
		// grok sends no standard ACP `agentInfo`, which the protocol permits
		// (MADR 0137, ninth amendment).
		AgentVersion string `json:"agentVersion"`
		ModelState   struct {
			CurrentModelID  string           `json:"currentModelId"`
			AvailableModels []availableModel `json:"availableModels"`
		} `json:"modelState"`
	} `json:"_meta"`
}

// initializeMeta is grok's Spec.InitializeMeta: its engine version, the
// model it runs and its catalog, from initialize's `_meta`.
func initializeMeta(meta map[string]any) acpagent.InitializeInfo {
	m := decodeGrokInitializeMeta(meta).Meta
	info := acpagent.InitializeInfo{
		Version:             m.AgentVersion,
		CurrentModel:        m.ModelState.CurrentModelID,
		DefaultAuthMethodID: m.DefaultAuthMethodID,
	}
	if len(m.ModelState.AvailableModels) > 0 {
		cat := modelsToCatalog(m.ModelState.CurrentModelID, m.ModelState.AvailableModels)
		info.Models = &cat
	}
	return info
}

// decodeGrokInitializeMeta reads grok's vendor block out of the _meta map the
// SDK keeps on its typed InitializeResponse.
//
// The SDK decodes _meta as map[string]any, so the typed view is recovered by a
// JSON round trip. Like the raw decode it replaces, it is best-effort: grok's
// fields are optional extensions, and an agent that sends none of them — or
// sends them in an unexpected shape — must not fail initialize.
func decodeGrokInitializeMeta(meta map[string]any) grokInitializeMeta {
	var out grokInitializeMeta
	if len(meta) == 0 {
		return out
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out.Meta)
	return out
}

func modelsToCatalog(currentID string, models []availableModel) picker.Catalog {
	opts := make([]picker.Option, 0, len(models))
	for _, m := range models {
		label := m.Name
		if label == "" {
			label = m.ModelID
		}
		opt := picker.Option{
			ID:    m.ModelID,
			Label: label,
			Group: "xai",
		}
		// Gate on supportsReasoningEffort: if false, emit no levels even when
		// a list is present — the CLI flag is the authority, not the array
		// (MADR 0052 §2.2 / A3.1).
		if m.Meta.SupportsReasoningEffort {
			levels := make([]picker.ThinkingLevel, 0, len(m.Meta.ReasoningEfforts))
			for _, e := range m.Meta.ReasoningEfforts {
				// Prefer Value over ID for the wire value — Value is what
				// --reasoning-effort takes; they are equal in the measured
				// payload but Value is the documented flag argument.
				id := e.Value
				if id == "" {
					id = e.ID
				}
				if id == "" {
					continue
				}
				levels = append(levels, picker.ThinkingLevel{
					ID:          id,
					Label:       e.Label,
					Description: e.Description,
					Default:     e.Default,
				})
			}
			opt.ThinkingLevels = picker.NormalizeThinkingLevels(levels)
		}
		opts = append(opts, opt)
	}
	var defaults []string
	if currentID != "" {
		defaults = []string{currentID}
	}
	return picker.Catalog{
		Kind:        picker.KindSingle,
		Source:      picker.SourceLive,
		Options:     opts,
		DefaultIDs:  defaults,
		AllowCustom: true,
		MaxSelect:   1,
	}.Normalize()
}
