package acpagent

import (
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// The frames below are raw JSON on purpose: a null must reach the SDK's
// decoder as an agent sends it. acp-go-sdk2 v1.14.0 keeps a received null
// apart from an omitted field (Patch[T]); v0.17.1 read both as absent
// (MADR 0184, amendment of 2026-10-15, E2–E4).

const compactionC1 = `{"sessionUpdate":"compaction_update","compactionId":"c1","status":"failed"`

// TestCompactionFollowsNull is PLAN 0184 B1 step 3 (E2): an omitted summary or
// error keeps its value, and a null clears it, as ACP's patch semantics say.
func TestCompactionFollowsNull(t *testing.T) {
	s, _ := newUpdateSession()
	steps := []struct {
		name, frame    string
		summary, error string
	}{
		{"both set", compactionC1 + `,"summary":[{"type":"text","text":"Kept"}],"error":"boom"}`, "Kept", "boom"},
		{"both omitted keep their values", compactionC1 + `}`, "Kept", "boom"},
		{"summary null clears only the summary", compactionC1 + `,"summary":null}`, "", "boom"},
		{"error null clears the error", compactionC1 + `,"error":null}`, "", ""},
	}
	for _, st := range steps {
		deliver(t, s, st.frame)
		got := lastCompaction(t, s)
		if got.Summary != st.summary || got.Error != st.error {
			t.Fatalf("%s: summary %q error %q; want %q and %q", st.name, got.Summary, got.Error, st.summary, st.error)
		}
	}
}

// A null _meta carries nothing to decline; a value is still declined (E2).
func TestCompactionMetaNullIsNotDeclined(t *testing.T) {
	s, h := newUpdateSession()
	deliver(t, s, compactionC1+`,"_meta":null}`)
	if variants, _ := h.declined(); len(variants) != 0 {
		t.Fatalf("_meta: null declined %v; want nothing", variants)
	}
	deliver(t, s, `{"sessionUpdate":"compaction_update","compactionId":"c2","status":"completed","_meta":{"y":2}}`)
	if variants, _ := h.declined(); !slices.Equal(variants, []string{"compaction_update._meta"}) {
		t.Fatalf("a _meta value declined %v; want [compaction_update._meta]", variants)
	}
}

// TestSessionInfoTitleNullIsDeclined is E3: the agent clearing its generated
// title is named once and changes nothing, since Magic has no rule for what an
// untitled session is then called (MADR 0187 D1).
func TestSessionInfoTitleNullIsDeclined(t *testing.T) {
	s, h := newUpdateSession()
	deliver(t, s, `{"sessionUpdate":"session_info_update","title":null}`)
	for len(s.events) > 0 {
		if ev := <-s.events; ev.Type == event.TypeSessionTitle {
			t.Fatalf("title: null emitted session_title %q", ev.Title)
		}
	}
	if variants, _ := h.declined(); !slices.Equal(variants, []string{"session_info_update.title_null"}) {
		t.Fatalf("title: null declined %v; want [session_info_update.title_null]", variants)
	}
}

// updatedAt is declined whether it carries a value or a null (E4).
func TestSessionInfoUpdatedAtIsDeclinedAsValueOrNull(t *testing.T) {
	for _, frame := range []string{
		`{"sessionUpdate":"session_info_update","updatedAt":"2026-10-15T00:00:00Z"}`,
		`{"sessionUpdate":"session_info_update","updatedAt":null}`,
	} {
		s, h := newUpdateSession()
		deliver(t, s, frame)
		if variants, _ := h.declined(); !slices.Equal(variants, []string{"session_info_update.updated_at"}) {
			t.Errorf("%s declined %v; want [session_info_update.updated_at]", frame, variants)
		}
	}
}
