package config

import (
	"reflect"
	"strings"
	"testing"
)

// MADR 0155 P1. HasInlineSecret decides whether a world-readable config is
// fatal or merely a warning, so these tables are the difference between a
// security control and a nuisance.

func TestHasInlineSecret(t *testing.T) {
	cases := []struct {
		name   string
		inFile map[string]bool
		want   bool
	}{
		{name: "nothing in the file", inFile: map[string]bool{}, want: false},
		{name: "the secret is in the file", inFile: map[string]bool{"relay.secret": true}, want: true},
		{
			// The case that makes the oracle necessary rather than decorative:
			// relay.url and relay.host_id in YAML with the secret supplied by
			// MCREMOTE_RELAY_SECRET is the arrangement RelayConfig's own doc
			// comment recommends. Nothing sensitive is on disk, so a
			// world-readable config here must not stop the daemon.
			name:   "url and host_id in the file, secret from the environment",
			inFile: map[string]bool{"relay.url": true, "relay.host_id": true},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasInlineSecret(func(key string) bool { return tc.inFile[key] })
			if got != tc.want {
				t.Errorf("HasInlineSecret = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("nil oracle is not a secret", func(t *testing.T) {
		if HasInlineSecret(nil) {
			t.Error("a nil oracle must not report a secret")
		}
	})
}

// TestSecretConfigKeysCoversEveryTag is PLAN 0155 C3's guard.
//
// It walks Config for mapstructure tags that name a credential and fails if one
// is not in secretConfigKeys. The failure mode it exists for is silent: a new
// secret field ships, HasInlineSecret keeps returning false for it, and a
// world-readable config carrying that secret starts the daemon with a warning
// instead of refusing.
//
// It matches on tag names rather than on types because a credential is a string
// like every other setting; the name is the only signal available. That makes
// it a heuristic, and a deliberately loud one — a field it flags wrongly is
// fixed by naming it in the exemption list below, with a reason.
func TestSecretConfigKeysCoversEveryTag(t *testing.T) {
	// Tag substrings that suggest a credential.
	suspicious := []string{"secret", "token", "password", "passwd", "api_key", "apikey", "credential"}

	// Tags that match a suspicious substring but hold no credential. Each needs
	// a reason, so that adding one is a decision rather than a reflex.
	exempt := map[string]string{
		"require_device_token": "a bool: whether a token is required, never a token",
		"keyring_disabled":     "a bool: whether the OS keyring is used",
	}

	listed := make(map[string]bool, len(secretConfigKeys))
	for _, k := range secretConfigKeys {
		listed[k] = true
	}

	var walk func(t reflect.Type, prefix string)
	var missing []string
	seen := map[reflect.Type]bool{}
	walk = func(rt reflect.Type, prefix string) {
		if rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			key := tag
			if prefix != "" {
				key = prefix + "." + tag
			}
			lower := strings.ToLower(tag)
			for _, s := range suspicious {
				if strings.Contains(lower, s) {
					if _, ok := exempt[tag]; !ok && !listed[key] {
						missing = append(missing, key)
					}
					break
				}
			}
			walk(f.Type, key)
		}
	}
	walk(reflect.TypeOf(Config{}), "")

	if len(missing) > 0 {
		t.Fatalf("config keys look like credentials but are not in secretConfigKeys: %v\n"+
			"Add them there, or add them to this test's exemption list with a reason (MADR 0155 D7).", missing)
	}

	// The guard must also be reaching the fields it claims to check: if the walk
	// silently stopped, the assertion above passes for the wrong reason.
	if !listed["relay.secret"] {
		t.Fatal("relay.secret is not in secretConfigKeys; the list is wrong")
	}
	if len(seen) < 5 {
		t.Fatalf("walked only %d struct types; the reflection walk is not reaching the config tree", len(seen))
	}
}
