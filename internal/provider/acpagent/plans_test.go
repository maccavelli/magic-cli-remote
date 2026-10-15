package acpagent

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// The frames below are built from the SDK's own types. They are the wire
// fixture for MADR 0192 D7 until a probe captures real ones: no agent Magic
// runs sends identified plans yet.

func planFrame(t *testing.T, c acp.PlanUpdateContent, meta map[string]any) string {
	t.Helper()
	b, err := json.Marshal(acp.SessionUpdate{PlanUpdate: &acp.SessionPlanUpdate{Plan: c, Meta: meta}})
	if err != nil {
		t.Fatalf("marshal plan_update: %v", err)
	}
	return string(b)
}

func removedFrame(t *testing.T, id string, meta map[string]any) string {
	t.Helper()
	b, err := json.Marshal(acp.SessionUpdate{PlanRemoved: &acp.SessionUpdatePlanRemoved{PlanId: acp.PlanId(id), Meta: meta}})
	if err != nil {
		t.Fatalf("marshal plan_removed: %v", err)
	}
	return string(b)
}

// drainPlans returns every identified_plan event, and how many plan events
// (the stable plan) were emitted alongside them.
func drainPlans(s *session) (identified []event.Event, stable int) {
	for len(s.events) > 0 {
		ev := <-s.events
		switch ev.Type {
		case event.TypeIdentifiedPlan:
			identified = append(identified, ev)
		case event.TypePlan:
			stable++
		}
	}
	return identified, stable
}

// onePlan delivers frame and returns the single identified_plan event it
// produced, failing if it produced anything else.
func onePlan(t *testing.T, frame string) event.Event {
	t.Helper()
	s, _ := newUpdateSession()
	deliver(t, s, frame)
	got, stable := drainPlans(s)
	if len(got) != 1 || stable != 0 {
		t.Fatalf("%s gave %d identified plan(s) and %d stable plan(s); want 1 and 0", frame, len(got), stable)
	}
	return got[0]
}

// TestIdentifiedPlanUpdates is PLAN 0192 P1 (MADR 0192 D1, D2): each content
// shape becomes an identified_plan event carrying its ID and shape, and a
// removal names only its own ID.
func TestIdentifiedPlanUpdates(t *testing.T) {
	t.Run("items", func(t *testing.T) {
		ev := onePlan(t, planFrame(t, acp.PlanUpdateContent{Items: &acp.PlanUpdateContentItems{
			PlanId: "p1",
			Entries: []acp.PlanEntry{
				{Content: "Write the tests", Status: acp.PlanEntryStatusCompleted, Priority: acp.PlanEntryPriorityHigh},
				{Content: "Ship", Status: acp.PlanEntryStatusPending, Priority: acp.PlanEntryPriorityLow},
			},
		}}, nil))
		want := []event.PlanEntry{
			{Content: "Write the tests", Status: event.PlanStatusCompleted, Priority: event.PlanPriorityHigh},
			{Content: "Ship", Status: event.PlanStatusPending, Priority: event.PlanPriorityLow},
		}
		if ev.PlanID != "p1" || ev.PlanKind != event.PlanKindItems || !slices.Equal(ev.Entries, want) {
			t.Fatalf("items plan = id %q kind %q entries %+v", ev.PlanID, ev.PlanKind, ev.Entries)
		}
		if ev.PlanContent != "" || ev.PlanURI != "" || ev.PlanRemoved || ev.PlanTruncated {
			t.Fatalf("items plan carries another shape's fields: %+v", ev)
		}
	})
	t.Run("markdown", func(t *testing.T) {
		ev := onePlan(t, planFrame(t, acp.PlanUpdateContent{Markdown: &acp.PlanUpdateContentMarkdown{
			PlanId: "p2", Content: "# Plan\n\n1. Read\n2. Write",
		}}, nil))
		if ev.PlanID != "p2" || ev.PlanKind != event.PlanKindMarkdown || ev.PlanContent != "# Plan\n\n1. Read\n2. Write" {
			t.Fatalf("markdown plan = id %q kind %q content %q", ev.PlanID, ev.PlanKind, ev.PlanContent)
		}
		if ev.PlanTruncated || ev.Entries != nil || ev.PlanURI != "" {
			t.Fatalf("markdown plan carries another shape's fields: %+v", ev)
		}
	})
	t.Run("file", func(t *testing.T) {
		ev := onePlan(t, planFrame(t, acp.PlanUpdateContent{File: &acp.PlanUpdateContentFile{
			PlanId: "p3", Uri: "file:///home/<user>/project/PLAN.md",
		}}, nil))
		if ev.PlanID != "p3" || ev.PlanKind != event.PlanKindFile || ev.PlanURI != "file:///home/<user>/project/PLAN.md" {
			t.Fatalf("file plan = id %q kind %q uri %q", ev.PlanID, ev.PlanKind, ev.PlanURI)
		}
		if ev.PlanContent != "" || ev.Entries != nil {
			t.Fatalf("a file plan must not be read: %+v", ev)
		}
	})
	t.Run("removed", func(t *testing.T) {
		ev := onePlan(t, removedFrame(t, "p2", nil))
		if ev.PlanID != "p2" || !ev.PlanRemoved || ev.PlanKind != "" || ev.Entries != nil {
			t.Fatalf("removal = %+v; want plan_id p2 and plan_removed only", ev)
		}
	})
}

// A Markdown plan is agent-chosen text, so it is bounded, cut on a rune
// boundary, and marked when anything was lost (MADR 0192 D1).
func TestIdentifiedPlanMarkdownIsCapped(t *testing.T) {
	exact := strings.Repeat("a", maxPlanContent)
	ev := onePlan(t, planFrame(t, acp.PlanUpdateContent{Markdown: &acp.PlanUpdateContentMarkdown{PlanId: "p", Content: exact}}, nil))
	if ev.PlanContent != exact || ev.PlanTruncated {
		t.Fatalf("a plan exactly at the cap was changed: %d bytes, truncated %t", len(ev.PlanContent), ev.PlanTruncated)
	}

	// A three-byte rune straddles the cap, so a byte cut would split it.
	over := strings.Repeat("a", maxPlanContent-1) + "€" + "tail"
	ev = onePlan(t, planFrame(t, acp.PlanUpdateContent{Markdown: &acp.PlanUpdateContentMarkdown{PlanId: "p", Content: over}}, nil))
	if !ev.PlanTruncated {
		t.Fatal("an over-long plan must be marked truncated")
	}
	if len(ev.PlanContent) > maxPlanContent || !utf8.ValidString(ev.PlanContent) {
		t.Fatalf("truncated plan is %d bytes, valid UTF-8 %t; want at most %d and valid",
			len(ev.PlanContent), utf8.ValidString(ev.PlanContent), maxPlanContent)
	}
	if ev.PlanContent != strings.Repeat("a", maxPlanContent-1) {
		t.Fatalf("truncated plan kept %d bytes, want the %d before the split rune", len(ev.PlanContent), maxPlanContent-1)
	}
}

// What Magic does not carry is declined by name, never dropped silently
// (MADR 0184 D7, 0192 D1).
func TestIdentifiedPlanDeclines(t *testing.T) {
	meta := map[string]any{"vendor": "x"}
	for _, tc := range []struct {
		name     string
		frame    func(t *testing.T) string
		emits    bool
		declines string
	}{
		{"update _meta", func(t *testing.T) string {
			return planFrame(t, acp.PlanUpdateContent{Markdown: &acp.PlanUpdateContentMarkdown{PlanId: "p", Content: "x"}}, meta)
		}, true, "plan_update._meta"},
		{"content _meta", func(t *testing.T) string {
			return planFrame(t, acp.PlanUpdateContent{Items: &acp.PlanUpdateContentItems{PlanId: "p", Meta: meta}}, nil)
		}, true, "plan_update._meta"},
		{"unknown content type", func(t *testing.T) string {
			return planFrame(t, acp.PlanUpdateContent{Unknown: json.RawMessage(`{"type":"diagram","planId":"p"}`)}, nil)
		}, false, "plan_update.diagram"},
		{"removal _meta", func(t *testing.T) string { return removedFrame(t, "p", meta) }, true, "plan_removed._meta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h := newUpdateSession()
			deliver(t, s, tc.frame(t))
			got, stable := drainPlans(s)
			if (len(got) == 1) != tc.emits || len(got) > 1 || stable != 0 {
				t.Fatalf("emitted %d identified and %d stable plan(s); want identified %t", len(got), stable, tc.emits)
			}
			if variants, _ := h.declined(); !slices.Equal(variants, []string{tc.declines}) {
				t.Fatalf("declined %v, want [%s]", variants, tc.declines)
			}
		})
	}
}

// TestPlanRemovedKeepsTheStablePlan replaces the 2026-07-21 test that pinned
// plan_removed as clearing the stable plan. Under schema 1.25 it removes one
// identified plan, so the stable plan must survive it (MADR 0192 D2).
func TestPlanRemovedKeepsTheStablePlan(t *testing.T) {
	s, _ := newUpdateSession()
	deliver(t, s, `{"sessionUpdate":"plan","entries":[{"content":"Keep me","status":"pending","priority":"high"}]}`)
	if ev := <-s.events; ev.Type != event.TypePlan || len(ev.Entries) != 1 {
		t.Fatalf("stable plan event = %+v", ev)
	}
	deliver(t, s, removedFrame(t, "other-plan", nil))
	got, stable := drainPlans(s)
	if stable != 0 {
		t.Fatalf("plan_removed emitted %d plan event(s), which a phone reads as replacing the stable plan", stable)
	}
	if len(got) != 1 || got[0].PlanID != "other-plan" || !got[0].PlanRemoved {
		t.Fatalf("plan_removed gave %+v; want one removal of other-plan", got)
	}
}

// fieldTags lists a struct's fields as "Name:json-tag".
func fieldTags(rt reflect.Type) []string {
	var out []string
	for i := range rt.NumField() {
		f := rt.Field(i)
		out = append(out, f.Name+":"+f.Tag.Get("json"))
	}
	return out
}

// TestPlanUpdateContentVariantsArePinned guards the unstable capability (MADR
// 0192 D6): an SDK bump that adds, removes or renames a plan shape or one of
// its fields fails here before it reaches the phone.
func TestPlanUpdateContentVariantsArePinned(t *testing.T) {
	for _, tc := range []struct {
		rt   reflect.Type
		want []string
	}{
		{reflect.TypeFor[acp.PlanUpdateContent](), []string{"Items:-", "File:-", "Markdown:-", "Unknown:-"}},
		{reflect.TypeFor[acp.PlanUpdateContentItems](), []string{"Meta:_meta,omitempty", "Entries:entries", "PlanId:planId", "Type:type"}},
		{reflect.TypeFor[acp.PlanUpdateContentMarkdown](), []string{"Meta:_meta,omitempty", "Content:content", "PlanId:planId", "Type:type"}},
		{reflect.TypeFor[acp.PlanUpdateContentFile](), []string{"Meta:_meta,omitempty", "PlanId:planId", "Type:type", "Uri:uri"}},
		{reflect.TypeFor[acp.SessionPlanUpdate](), []string{"Meta:_meta,omitempty", "Plan:plan", "SessionUpdate:sessionUpdate"}},
		{reflect.TypeFor[acp.SessionUpdatePlanRemoved](), []string{"Meta:_meta,omitempty", "PlanId:planId", "SessionUpdate:sessionUpdate"}},
	} {
		if got := fieldTags(tc.rt); !slices.Equal(got, tc.want) {
			t.Errorf("acp.%s fields = %v\nwant %v\nthe unstable plan schema changed: review MADR 0192 before updating this pin",
				tc.rt.Name(), got, tc.want)
		}
	}
}
