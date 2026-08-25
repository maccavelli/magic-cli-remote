package opencode

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// modelModalities is the 1.18.21 `Model.capabilities.input` / `.output` object.
// Every field is a fact the engine advertised; absent means false, and false is
// never upgraded by inferring one modality from another.
type modelModalities struct {
	Text  bool `json:"text"`
	Audio bool `json:"audio"`
	Image bool `json:"image"`
	Video bool `json:"video"`
	PDF   bool `json:"pdf"`
}

// modelCapabilities is the 1.18.21 `Model.capabilities` object.
//
// SECURITY/scope: cost tables, provider configuration and the interleaved
// reasoning field are deliberately absent. MADR 0112 A2 advertises what a model
// accepts, not what it charges or how its provider is wired, and a field this
// struct does not declare cannot reach a picker row (the same mechanism that
// keeps API keys out of [connectedProvidersResponse]).
type modelCapabilities struct {
	Reasoning  bool            `json:"reasoning"`
	Attachment bool            `json:"attachment"`
	ToolCall   bool            `json:"toolcall"`
	Input      modelModalities `json:"input"`
	Output     modelModalities `json:"output"`
}

// modelSurface is the sanitized per-model view the phone gates its composer on:
// which inputs the model accepts, and which reasoning rungs it advertises.
type modelSurface struct {
	Attachment bool
	ToolCall   bool
	Reasoning  bool
	Inputs     picker.ModelInputs
	Levels     []picker.ThinkingLevel
}

// isJSONObject reports whether raw is a JSON object.
//
// The 1.18.21 schema types `Model.variants` as Record<string, object>. A value
// that is not an object is malformed for that schema, so it is dropped rather
// than promoted into a rung the model never advertised — inventing a level is
// the one thing MADR 0052 §1 forbids outright.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// decodeVariants turns the optional 1.18.21 `Model.variants` record into
// thinking rungs (MADR 0112 A14).
//
// The upstream key is the rung ID verbatim: no Label, no Description, and no
// Default, because the variants schema carries no display text and no default
// marker. Keys are trimmed, empty-after-trim keys are dropped, and the first
// spelling of a duplicate-after-trim key wins. Map iteration order is not
// stable in Go, so keys are sorted before normalization to make the result
// deterministic for a given catalog.
func decodeVariants(raw map[string]json.RawMessage) []picker.ThinkingLevel {
	if len(raw) == 0 {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	levels := make([]picker.ThinkingLevel, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		id := strings.TrimSpace(k)
		if id == "" {
			continue
		}
		if !isJSONObject(raw[k]) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		levels = append(levels, picker.ThinkingLevel{ID: id})
	}
	if len(levels) == 0 {
		return nil
	}
	return picker.NormalizeThinkingLevels(levels)
}

// surface decodes one engine model's advertised capability and variant halves.
// A model that advertises neither yields the zero surface, which is the honest
// "nothing advertised" answer rather than a guess.
func (m providerModel) surface() modelSurface {
	s := modelSurface{Levels: decodeVariants(m.Variants)}
	if m.Capabilities == nil {
		return s
	}
	c := m.Capabilities
	s.Attachment, s.ToolCall, s.Reasoning = c.Attachment, c.ToolCall, c.Reasoning
	s.Inputs = picker.ModelInputs{
		Text:  c.Input.Text,
		Image: c.Input.Image,
		Audio: c.Input.Audio,
		Video: c.Input.Video,
		PDF:   c.Input.PDF,
	}
	return s
}

// hasInputs reports whether the engine advertised an input capability block at
// all. Nil Inputs on a picker option means "unknown", so an all-false block
// must still be carried: "this model accepts only text" and "this model did not
// say" are different answers and the composer treats them differently.
func (s modelSurface) hasInputs() bool {
	return s.Inputs != picker.ModelInputs{}
}

// apply copies the surface onto a picker row.
func (s modelSurface) apply(opt picker.Option) picker.Option {
	opt.Attachment, opt.ToolCall, opt.Reasoning = s.Attachment, s.ToolCall, s.Reasoning
	opt.ThinkingLevels = s.Levels
	if s.hasInputs() {
		in := s.Inputs
		opt.Inputs = &in
	}
	return opt
}

// modelSurfaceCache holds decoded surfaces by full "provider/model" ID.
//
// It is written once per catalog refresh (AfterBoot) and read on every prompt,
// model change and capability emission, so it takes an RWMutex rather than
// borrowing the dialect's single mutex.
type modelSurfaceCache struct {
	mu   sync.RWMutex
	byID map[string]modelSurface
}

// replace swaps the whole cache. A refresh that decoded nothing leaves the
// previous surfaces in place: an empty catalog fetch is a failure to learn, not
// evidence that every model lost its capabilities.
func (c *modelSurfaceCache) replace(byID map[string]modelSurface) {
	if len(byID) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID = byID
}

// lookup returns the surface for a full "provider/model" ID.
func (c *modelSurfaceCache) lookup(fullID string) (modelSurface, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byID[fullID]
	return s, ok
}

// len reports how many models the cache knows, for logging only.
func (c *modelSurfaceCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byID)
}

// DecodeVariantsForTest exposes the variant decoder to the live-tagged gates,
// which live in the external opencode_test package. It is test support for the
// A14 contract, not a production entry point.
func DecodeVariantsForTest(raw map[string]json.RawMessage) []picker.ThinkingLevel {
	return decodeVariants(raw)
}
