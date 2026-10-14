package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// recordingHandler keeps every log record, so a test can ask what was said.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// declined returns the variant attribute of every "declined session update"
// record, and how many records were the old silent default.
func (h *recordingHandler) declined() (variants []string, silent int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		switch r.Message {
		case declinedUpdateMsg:
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "variant" {
					variants = append(variants, a.Value.String())
				}
				return true
			})
		case "unhandled session update":
			silent++
		}
	}
	return variants, silent
}

// updateRow is one session/update payload and what Magic must do with it:
// emit an event of type emits, decline the variants in declines, or (both
// empty) drop it on purpose.
type updateRow struct {
	update   string
	emits    event.Type
	declines []string
}

const text = `{"type":"text","text":"x"}`

// sessionUpdateRows has one row per acp.SessionUpdate field, keyed by the
// field's name, so a variant added to the SDK has no row and fails
// TestEverySessionUpdateIsMappedOrDeclined.
var sessionUpdateRows = map[string]updateRow{
	"UserMessageChunk":  {update: `{"sessionUpdate":"user_message_chunk","content":` + text + `}`},
	"AgentMessageChunk": {update: `{"sessionUpdate":"agent_message_chunk","content":` + text + `}`, emits: event.TypeAssistantChunk},
	"AgentThoughtChunk": {update: `{"sessionUpdate":"agent_thought_chunk","content":` + text + `}`, emits: event.TypeThoughtChunk},
	"ToolCall":          {update: `{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read"}`, emits: event.TypeToolCall},
	// Terminal, because the tool lane holds a running update for its coalesce
	// window (chunkbuf.WithToolLane).
	"ToolCallUpdate": {update: `{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed"}`, emits: event.TypeToolUpdate},
	"Plan":           {update: `{"sessionUpdate":"plan","entries":[]}`, emits: event.TypePlan},
	"PlanUpdate": {update: `{"sessionUpdate":"plan_update","plan":{"type":"markdown","planId":"p1","content":"x"}}`,
		declines: []string{"plan_update"}},
	"PlanRemoved": {update: `{"sessionUpdate":"plan_removed","planId":"p1"}`, emits: event.TypePlan},
	"AvailableCommandsUpdate": {update: `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"web","description":"search"}]}`,
		emits: event.TypeAvailableCommands},
	"CurrentModeUpdate": {update: `{"sessionUpdate":"current_mode_update","currentModeId":"plan"}`, emits: event.TypeMode},
	"ConfigOptionUpdate": {update: `{"sessionUpdate":"config_option_update","configOptions":[{"type":"boolean","id":"b","name":"B","currentValue":true}]}`,
		emits: event.TypeSessionConfig},
	"SessionInfoUpdate": {update: `{"sessionUpdate":"session_info_update","title":"t","updatedAt":"2026-10-13T00:00:00Z"}`,
		emits: event.TypeSessionTitle, declines: []string{"session_info_update.updated_at"}}, // MADR 0187 D1
	"UsageUpdate": {update: `{"sessionUpdate":"usage_update","used":1,"size":2,"cost":{"amount":0.5,"currency":"USD"}}`,
		emits: event.TypeUsage, declines: []string{"usage_update.cost"}},
	"Notice":                 {update: `{"sessionUpdate":"notice","severity":"info","title":"t"}`, emits: event.TypeAgentNotice}, // MADR 0188 D1
	"CompactionUpdate":       {update: `{"sessionUpdate":"compaction_update","compactionId":"c1","status":"completed"}`, declines: []string{"compaction_update"}},
	"CompactionSummaryChunk": {update: `{"sessionUpdate":"compaction_summary_chunk","compactionId":"c1","content":` + text + `}`, declines: []string{"compaction_summary_chunk"}},
	"SubagentUpdate":         {update: `{"sessionUpdate":"subagent_update","sessionId":"child-1"}`, declines: []string{"subagent_update"}},
	"SessionMessage":         {update: `{"sessionUpdate":"session_message","messageId":"m1"}`, declines: []string{"session_message"}},
	"SessionMessageChunk":    {update: `{"sessionUpdate":"session_message_chunk","messageId":"m1","content":` + text + `}`, declines: []string{"session_message_chunk"}},
	"Unknown":                {update: `{"sessionUpdate":"future_update","x":1}`, declines: []string{"unknown:future_update"}},
}

func newUpdateSession() (*session, *recordingHandler) {
	h := &recordingHandler{}
	return &session{
		localID: "local-1",
		log:     slog.New(h),
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
	}, h
}

func deliver(t *testing.T, s *session, update string) {
	t.Helper()
	var n acp.SessionNotification
	if err := json.Unmarshal([]byte(`{"sessionId":"agent-1","update":`+update+`}`), &n); err != nil {
		t.Fatalf("decode %s: %v", update, err)
	}
	if err := s.SessionUpdate(context.Background(), n); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
}

// TestEverySessionUpdateIsMappedOrDeclined is PLAN 0184 A4 step 1 (MADR D7):
// every schema 1.25 session-update variant, plus Unknown, is either mapped to
// an event, dropped on purpose, or declined with a Debug record naming it.
// None reaches a silent default.
func TestEverySessionUpdateIsMappedOrDeclined(t *testing.T) {
	ut := reflect.TypeFor[acp.SessionUpdate]()
	for i := range ut.NumField() {
		if _, ok := sessionUpdateRows[ut.Field(i).Name]; !ok {
			t.Errorf("acp.SessionUpdate.%s has no row: map it or decline it", ut.Field(i).Name)
		}
	}
	for field, row := range sessionUpdateRows {
		t.Run(field, func(t *testing.T) {
			s, h := newUpdateSession()
			var n acp.SessionNotification
			if err := json.Unmarshal([]byte(`{"sessionId":"agent-1","update":`+row.update+`}`), &n); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if f := reflect.ValueOf(n.Update).FieldByName(field); f.IsNil() {
				t.Fatalf("the payload did not decode as %s", field)
			}
			deliver(t, s, row.update)
			deliver(t, s, row.update) // a decline is logged once per session

			var got []event.Type
			for len(s.events) > 0 {
				got = append(got, (<-s.events).Type)
			}
			switch {
			case row.emits == "" && len(got) != 0:
				t.Errorf("events = %v, want none", got)
			case row.emits != "" && (len(got) == 0 || got[0] != row.emits):
				t.Errorf("events = %v, want %s", got, row.emits)
			}
			variants, silent := h.declined()
			if silent != 0 {
				t.Errorf("%d update(s) fell to the silent default", silent)
			}
			if !reflect.DeepEqual(variants, row.declines) && (len(variants) != 0 || len(row.declines) != 0) {
				t.Errorf("declined %v, want %v (once each)", variants, row.declines)
			}
		})
	}
}

// TestUnknownUpdateNamesAreBounded: an agent picks Unknown's names, so the
// set of them a session remembers is capped.
func TestUnknownUpdateNamesAreBounded(t *testing.T) {
	s, h := newUpdateSession()
	for i := range maxUnknownUpdateNames + 10 {
		deliver(t, s, `{"sessionUpdate":"future_`+string(rune('a'+i%26))+string(rune('a'+i/26))+`"}`)
	}
	variants, _ := h.declined()
	if len(variants) != maxUnknownUpdateNames {
		t.Fatalf("logged %d unknown names, want the cap %d", len(variants), maxUnknownUpdateNames)
	}
}
