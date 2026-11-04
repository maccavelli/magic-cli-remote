package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// unboundKeys are Config keys deliberately given no viper default, each with
// its reason. Without a default a key is absent from viper's key set, and
// AutomaticEnv ignores its MCREMOTE_* variable (MADR 0201 Deviation 5).
var unboundKeys = map[string]string{
	"tls.letsencrypt.domains": "string list: config file or --tls-domain only",
}

// configLeafKeys returns the dotted mapstructure key of every leaf of Config:
// a field that is not itself a struct of further keys.
func configLeafKeys() []string {
	var out []string
	var walk func(rt reflect.Type, prefix string)
	walk = func(rt reflect.Type, prefix string) {
		for f := range rt.Fields() {
			parts := strings.Split(f.Tag.Get("mapstructure"), ",")
			tag := parts[0]
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			// An embedded ",squash" struct contributes its fields at this
			// level.
			if tag == "" && slices.Contains(parts[1:], "squash") && ft.Kind() == reflect.Struct {
				walk(ft, prefix)
				continue
			}
			if tag == "" || tag == "-" {
				continue
			}
			key := tag
			if prefix != "" {
				key = prefix + "." + tag
			}
			if ft.Kind() == reflect.Struct && ft.PkgPath() == rt.PkgPath() {
				walk(ft, key)
				continue
			}
			out = append(out, key)
		}
	}
	walk(reflect.TypeFor[Config](), "")
	return out
}

// TestEveryConfigKeyHasAViperDefault is MADR 0201 D23 (0073 §6.4), and
// carries out 0090-PLAN-config-template-completeness.md Phase 3 Task 3.1.
// setDefaults had no default for several keys, so their environment
// variables were silently ignored.
func TestEveryConfigKeyHasAViperDefault(t *testing.T) {
	v := viper.New()
	setDefaults(v)
	known := v.AllKeys()
	var missing []string
	for _, key := range configLeafKeys() {
		if _, ok := unboundKeys[key]; ok {
			continue
		}
		bound := slices.Contains(known, key) || slices.ContainsFunc(known, func(k string) bool {
			return strings.HasPrefix(k, key+".")
		})
		if !bound {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("config keys with no viper default, so no MCREMOTE_* override (add a SetDefault, "+
			"or list the key in unboundKeys with its reason):\n  %s", strings.Join(missing, "\n  "))
	}
}
