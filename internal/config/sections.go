package config

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// ProviderSection is one providers.<id> section of the config file. Its
// provider's package owns the struct, its defaults and its validation (0208
// MADR D6, amendment of 2026-11-04); this package only loads it.
//
// The struct's fields carry mapstructure tags, as Config's do. Every leaf key
// gets its default from the value [ProviderSectionSpec.New] returns, and so
// its MCREMOTE_PROVIDERS_<ID>_<KEY> variable. A field tagged `env:"-"` is
// config-file only: it gets no default, and its variable is refused.
type ProviderSection interface {
	// IsEnabled reports providers.<id>.enabled.
	IsEnabled() bool
	// Validate checks the section once it is decoded.
	Validate() error
}

// ProviderSectionSpec names one section and builds it holding its defaults.
type ProviderSectionSpec struct {
	ID  string
	New func() ProviderSection
}

// ProviderSections are the decoded sections, by provider id.
type ProviderSections map[string]ProviderSection

// sectionLeaf is one key of a section.
type sectionLeaf struct {
	key   string
	value any
	noEnv bool
}

// sectionLeaves returns sec's keys, with their default values. An embedded
// ",squash" struct contributes its keys at its parent's level, and a struct
// field is a table of further keys; a slice or a map is one leaf.
func sectionLeaves(sec ProviderSection) []sectionLeaf {
	var out []sectionLeaf
	var walk func(rv reflect.Value, prefix string)
	walk = func(rv reflect.Value, prefix string) {
		rt := rv.Type()
		for i := range rt.NumField() {
			f := rt.Field(i)
			parts := strings.Split(f.Tag.Get("mapstructure"), ",")
			tag := parts[0]
			fv := rv.Field(i)
			if tag == "" && slices.Contains(parts[1:], "squash") && f.Type.Kind() == reflect.Struct {
				walk(fv, prefix)
				continue
			}
			if tag == "" || tag == "-" {
				continue
			}
			key := tag
			if prefix != "" {
				key = prefix + "." + tag
			}
			if f.Type.Kind() == reflect.Struct {
				walk(fv, key)
				continue
			}
			out = append(out, sectionLeaf{key: key, value: fv.Interface(), noEnv: f.Tag.Get("env") == "-"})
		}
	}
	walk(reflect.ValueOf(sec).Elem(), "")
	return out
}

// setSectionDefaults gives each section key its default, which is also what
// makes AutomaticEnv resolve the key's variable.
func setSectionDefaults(v *viper.Viper, specs []ProviderSectionSpec) {
	for _, spec := range specs {
		for _, leaf := range sectionLeaves(spec.New()) {
			if !leaf.noEnv {
				v.SetDefault("providers."+spec.ID+"."+leaf.key, leaf.value)
			}
		}
	}
}

// providerEnvPrefix is the prefix of every providers.* variable.
const providerEnvPrefix = "MCREMOTE_PROVIDERS_"

// sectionEnvName is the variable AutomaticEnv reads for providers.<id>.<key>.
func sectionEnvName(id, key string) string {
	return providerEnvPrefix + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(id+"."+key))
}

// decodeSections decodes, from v, the section of each spec, and validates it.
// A key the section does not have fails the load and is named, whether it is
// in the config file or in a variable: a typo or a retired key would
// otherwise be ignored without a word.
func decodeSections(v *viper.Viper, specs []ProviderSectionSpec, environ []string) (ProviderSections, error) {
	providers, _ := v.AllSettings()["providers"].(map[string]any)
	out := make(ProviderSections, len(specs))
	for _, spec := range specs {
		sec := spec.New()
		leaves := sectionLeaves(sec)
		if err := checkSectionEnv(spec.ID, leaves, environ); err != nil {
			return nil, err
		}
		raw, _ := providers[spec.ID].(map[string]any)
		if unknown := unknownSectionKeys(raw, leaves, ""); len(unknown) > 0 {
			return nil, fmt.Errorf("providers.%s has no key %s", spec.ID, strings.Join(unknown, ", "))
		}
		sub := viper.New()
		if err := sub.MergeConfigMap(raw); err != nil {
			return nil, fmt.Errorf("providers.%s: %w", spec.ID, err)
		}
		if err := sub.Unmarshal(sec); err != nil {
			return nil, fmt.Errorf("providers.%s: %w", spec.ID, err)
		}
		if err := sec.Validate(); err != nil {
			return nil, fmt.Errorf("providers.%s: %w", spec.ID, err)
		}
		out[spec.ID] = sec
	}
	return out, nil
}

// unknownSectionKeys returns the dotted keys of raw that are not a leaf of the
// section, nor a table holding one, sorted.
func unknownSectionKeys(raw map[string]any, leaves []sectionLeaf, prefix string) []string {
	var out []string
	for k, val := range raw {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if slices.ContainsFunc(leaves, func(l sectionLeaf) bool { return l.key == key }) {
			continue
		}
		table, isTable := val.(map[string]any)
		if isTable && slices.ContainsFunc(leaves, func(l sectionLeaf) bool { return strings.HasPrefix(l.key, key+".") }) {
			out = append(out, unknownSectionKeys(table, leaves, key)...)
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// checkSectionEnv refuses a MCREMOTE_PROVIDERS_<ID>_* variable that names no
// key of the section, or names a config-file-only one.
func checkSectionEnv(id string, leaves []sectionLeaf, environ []string) error {
	prefix := providerEnvPrefix + strings.ToUpper(id) + "_"
	var bad []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		i := slices.IndexFunc(leaves, func(l sectionLeaf) bool { return sectionEnvName(id, l.key) == name })
		switch {
		case i < 0:
			bad = append(bad, name+" names no providers."+id+" key")
		case leaves[i].noEnv:
			bad = append(bad, name+" sets providers."+id+"."+leaves[i].key+", which is config-file only")
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("%s", strings.Join(bad, "; "))
}
