// Package picker defines the shared interactive option catalog used across
// the control plane (models.list, future multi-select surfaces) and clients.
//
// The daemon is non-interactive; it only advertises catalogs. Clients render
// a picker UI and return selected option ids to the appropriate RPC.
package picker

import (
	"fmt"
	"slices"
	"strings"
)

// Kind is how many options the client may select.
type Kind string

const (
	// KindSingle allows at most one selected id.
	KindSingle Kind = "single"
	// KindMulti allows zero or more selected ids within Min/MaxSelect bounds.
	KindMulti Kind = "multi"
)

// Source describes where the catalog came from.
type Source string

const (
	// SourceLive was fetched from a running agent/engine.
	SourceLive Source = "live"
	// SourceStatic is a built-in / config fallback list.
	SourceStatic Source = "static"
	// SourceMerged combines live and static (live wins on id collision).
	SourceMerged Source = "merged"
)

// Option is one selectable row in a picker.
type Option struct {
	// ID is the stable value returned by the client (e.g. model id).
	ID string `json:"id"`
	// Label is the primary display line. Empty → clients may show ID.
	Label string `json:"label,omitempty"`
	// Description is optional secondary text.
	Description string `json:"description,omitempty"`
	// Group is an optional section header (e.g. "opencode", "anthropic").
	Group string `json:"group,omitempty"`
	// Enabled is false when the option is visible but not choosable.
	// Omitted/false-zero on the wire: clients treat missing as true when
	// unmarshaling with [Option.IsEnabled].
	Enabled *bool `json:"enabled,omitempty"`
	// Meta carries optional badges / kinds (e.g. permission kind).
	Meta map[string]string `json:"meta,omitempty"`
}

// IsEnabled reports whether the option may be selected. Nil Enabled means true.
func (o Option) IsEnabled() bool {
	if o.Enabled == nil {
		return true
	}
	return *o.Enabled
}

// WithEnabled returns a copy with Enabled set.
func WithEnabled(enabled bool) *bool {
	return &enabled
}

// Catalog is a full picker payload for one surface (e.g. models for a provider).
type Catalog struct {
	Kind Kind `json:"kind"`
	// Source is informational for clients (show "offline catalog" badge, etc.).
	Source Source `json:"source,omitempty"`
	// Options is the ordered list (client may re-sort for search).
	Options []Option `json:"options"`
	// DefaultIDs are pre-selected when the user has no prior choice.
	// KindSingle uses at most the first id that exists in Options.
	DefaultIDs []string `json:"default_ids,omitempty"`
	// AllowCustom permits a free-text value not in Options (models often true).
	AllowCustom bool `json:"allow_custom,omitempty"`
	// MinSelect / MaxSelect bound multi selection. For KindSingle, MaxSelect
	// is treated as 1. Zero MaxSelect on KindMulti means unlimited.
	MinSelect int `json:"min_select,omitempty"`
	MaxSelect int `json:"max_select,omitempty"`
}

// Normalize fills defaults so catalogs are safe to send on the wire.
func (c Catalog) Normalize() Catalog {
	if c.Kind == "" {
		c.Kind = KindSingle
	}
	if c.Kind == KindSingle {
		if c.MaxSelect == 0 || c.MaxSelect > 1 {
			c.MaxSelect = 1
		}
		if c.MinSelect > 1 {
			c.MinSelect = 1
		}
	}
	for i := range c.Options {
		if c.Options[i].Label == "" {
			c.Options[i].Label = c.Options[i].ID
		}
	}
	return c
}

// ValidateSelection checks selected ids against this catalog.
// Empty selection is allowed when MinSelect is 0 (default for optional picks).
// When AllowCustom is true, ids that are not in Options are still accepted.
func (c Catalog) ValidateSelection(ids []string) error {
	c = c.Normalize()
	// Dedupe while preserving order.
	seen := make(map[string]struct{}, len(ids))
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	n := len(clean)
	min := c.MinSelect
	max := c.MaxSelect
	if c.Kind == KindSingle {
		max = 1
	}
	if max == 0 && c.Kind == KindMulti {
		max = n // unlimited
	}
	if n < min {
		return fmt.Errorf("select at least %d option(s)", min)
	}
	if max > 0 && n > max {
		return fmt.Errorf("select at most %d option(s)", max)
	}

	byID := make(map[string]Option, len(c.Options))
	for _, o := range c.Options {
		byID[o.ID] = o
	}
	for _, id := range clean {
		o, ok := byID[id]
		if !ok {
			if c.AllowCustom {
				continue
			}
			return fmt.Errorf("unknown option %q", id)
		}
		if !o.IsEnabled() {
			return fmt.Errorf("option %q is disabled", id)
		}
	}
	return nil
}

// MergeLiveStatic builds a catalog preferring live options, then appending
// static options whose ids were not present live. DefaultIDs prefer live
// defaults when non-empty.
func MergeLiveStatic(live, static Catalog) Catalog {
	live = live.Normalize()
	static = static.Normalize()
	byID := make(map[string]struct{}, len(live.Options)+len(static.Options))
	out := make([]Option, 0, len(live.Options)+len(static.Options))
	for _, o := range live.Options {
		if o.ID == "" {
			continue
		}
		if _, ok := byID[o.ID]; ok {
			continue
		}
		byID[o.ID] = struct{}{}
		out = append(out, o)
	}
	for _, o := range static.Options {
		if o.ID == "" {
			continue
		}
		if _, ok := byID[o.ID]; ok {
			continue
		}
		byID[o.ID] = struct{}{}
		out = append(out, o)
	}
	kind := live.Kind
	if kind == "" {
		kind = static.Kind
	}
	if kind == "" {
		kind = KindSingle
	}
	defaults := live.DefaultIDs
	if len(defaults) == 0 {
		defaults = static.DefaultIDs
	}
	// Drop defaults that are no longer present unless custom is allowed.
	allowCustom := live.AllowCustom || static.AllowCustom
	if !allowCustom {
		defaults = slices.DeleteFunc(slices.Clone(defaults), func(id string) bool {
			_, ok := byID[id]
			return !ok
		})
	}
	src := SourceMerged
	if len(live.Options) == 0 {
		src = SourceStatic
	} else if len(static.Options) == 0 {
		src = SourceLive
	}
	return Catalog{
		Kind:        kind,
		Source:      src,
		Options:     out,
		DefaultIDs:  defaults,
		AllowCustom: allowCustom,
		MinSelect:   maxInt(live.MinSelect, static.MinSelect),
		MaxSelect:   mergeMax(live.MaxSelect, static.MaxSelect, kind),
	}.Normalize()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mergeMax(a, b int, kind Kind) int {
	if kind == KindSingle {
		return 1
	}
	// 0 means unlimited — prefer unlimited if either side is unlimited.
	if a == 0 || b == 0 {
		if a == 0 && b == 0 {
			return 0
		}
		if a == 0 {
			return b
		}
		return a
	}
	return maxInt(a, b)
}

// SingleCatalog is a convenience for static single-select lists.
func SingleCatalog(source Source, options []Option, defaultID string, allowCustom bool) Catalog {
	var defaults []string
	if defaultID != "" {
		defaults = []string{defaultID}
	}
	return Catalog{
		Kind:        KindSingle,
		Source:      source,
		Options:     options,
		DefaultIDs:  defaults,
		AllowCustom: allowCustom,
		MaxSelect:   1,
	}.Normalize()
}
