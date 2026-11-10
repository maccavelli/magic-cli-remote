package gobble

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
)

// errorLimit is the Spec's ErrorLimit: gobble's structured limit data in a
// failed prompt's error (MADR 0243 D3 source 1, D7).
func errorLimit(raw string) (agenterr.Limit, time.Time, bool) {
	return errorLimitAt(raw, time.Now())
}

// errorLimitAt reads the JSON-RPC error object in raw, decodes its data as
// acpext's ErrorData, and maps the limit, provider, code, provider message
// and reset time onto an agenterr.Limit (gobble-cli 0049-MADR). The reset is
// ResetAt, or now plus RetryAfterMs when ResetAt is absent.
//
// ok is false, and the text's reading stands, when raw holds no such object,
// its data is not an object, or the data describes no limit: no limit scope
// and a kind that is neither quota nor rate-limited. That keeps a provider
// outage, which also carries a provider and code, presented as it is today.
//
// gobble's "plan" names no window. When the text states one ("5-hour
// limit"), the text's scope is kept (MADR 0243 D3: a phrase refines plan).
func errorLimitAt(raw string, now time.Time) (agenterr.Limit, time.Time, bool) {
	i := strings.IndexByte(raw, '{')
	if i < 0 {
		return agenterr.Limit{}, time.Time{}, false
	}
	var obj struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(raw[i:])).Decode(&obj); err != nil || len(obj.Data) == 0 {
		return agenterr.Limit{}, time.Time{}, false
	}
	var d acpext.ErrorData
	if err := json.Unmarshal(obj.Data, &d); err != nil {
		return agenterr.Limit{}, time.Time{}, false
	}
	if d.Limit == "" && d.Kind != acpext.KindQuota && d.Kind != acpext.KindRateLimited {
		return agenterr.Limit{}, time.Time{}, false
	}
	l := agenterr.Limit{
		Scope:           string(d.Limit),
		Provider:        d.Provider,
		Code:            d.Code,
		ProviderMessage: d.ProviderMessage,
	}
	if d.Limit == acpext.LimitPlan {
		if text := agenterr.Present(raw, now).Limit.Scope; text != "" && text != agenterr.ScopePlan {
			l.Scope = ""
		}
	}
	var reset time.Time
	if t, err := time.Parse(time.RFC3339, d.ResetAt); err == nil {
		reset = t.UTC()
	} else if d.RetryAfterMs > 0 {
		reset = now.Add(time.Duration(d.RetryAfterMs) * time.Millisecond)
	}
	return l, reset, true
}
