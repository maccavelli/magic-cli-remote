package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestCollaborationEventJSONMergeShape(t *testing.T) {
	full := event.Event{
		Type: event.TypeCollaboration,
		CollaborationModes: []event.CollaborationMode{
			{ID: "plan", Name: "Plan"},
		},
		CurrentCollaborationModeID: "plan",
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	var got event.Event
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != event.TypeCollaboration || len(got.CollaborationModes) != 1 ||
		got.CurrentCollaborationModeID != "plan" {
		t.Fatalf("full = %+v", got)
	}
	curOnly := event.Event{Type: event.TypeCollaboration, CurrentCollaborationModeID: "default"}
	raw, _ = json.Marshal(curOnly)
	got = event.Event{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.CollaborationModes != nil || got.CurrentCollaborationModeID != "default" {
		t.Fatalf("current-only = %+v", got)
	}
	if err := json.Unmarshal([]byte(`{"type":"session_mode","current_mode_id":"auto"}`), &got); err != nil {
		t.Fatal(err)
	}
}

func TestTypeConstants(t *testing.T) {
	cases := []struct {
		name string
		typ  event.Type
		want string
	}{
		{"TypeSessionStatus", event.TypeSessionStatus, "session_status"},
		{"TypeUserMessage", event.TypeUserMessage, "user_message"},
		{"TypeAssistantChunk", event.TypeAssistantChunk, "assistant_message_chunk"},
		{"TypeThoughtChunk", event.TypeThoughtChunk, "thought_chunk"},
		{"TypeToolCall", event.TypeToolCall, "tool_call"},
		{"TypeToolUpdate", event.TypeToolUpdate, "tool_call_update"},
		{"TypePermission", event.TypePermission, "permission_request"},
		{"TypePermissionResolved", event.TypePermissionResolved, "permission_resolved"},
		{"TypeQuestion", event.TypeQuestion, "question_request"},
		{"TypeQuestionResolved", event.TypeQuestionResolved, "question_resolved"},
		{"TypeTurnComplete", event.TypeTurnComplete, "turn_complete"},
		{"TypeError", event.TypeError, "error"},
		{"TypeAvailableCommands", event.TypeAvailableCommands, "available_commands"},
		{"TypePlan", event.TypePlan, "plan"},
		{"TypeNotice", event.TypeNotice, "notice"},
		{"TypeUsage", event.TypeUsage, "usage_update"},
		{"TypeMode", event.TypeMode, "session_mode"},
		{"TypeCollaboration", event.TypeCollaboration, "collaboration_mode"},
		{"TypeGoal", event.TypeGoal, "session_goal"},
		{"TypeSessionConfig", event.TypeSessionConfig, "session_config"},
		{"TypeSessionCapabilities", event.TypeSessionCapabilities, "session_capabilities"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.typ) != tc.want {
				t.Fatalf("got %q, want %q", tc.typ, tc.want)
			}
		})
	}
}

func TestControlTypesComplete(t *testing.T) {
	control := []event.Type{
		event.TypeSessionStatus,
		event.TypePermission,
		event.TypePermissionResolved,
		event.TypeQuestion,
		event.TypeQuestionResolved,
		event.TypeTurnComplete,
		event.TypeError,
		event.TypeNotice,
		event.TypeToolCall,
		event.TypeToolUpdate,
		event.TypeUserMessage,
		event.TypeMode,
		event.TypeCollaboration,
		event.TypeGoal,
		event.TypeSessionConfig,
		event.TypeSessionCapabilities,
		event.TypePlan,
	}
	for _, tc := range control {
		t.Run(string(tc), func(t *testing.T) {
			if !event.IsControl(tc) {
				t.Fatalf("IsControl(%q) = false, want true", tc)
			}
		})
	}
}

func TestDroppableTypes(t *testing.T) {
	droppable := []event.Type{
		event.TypeAssistantChunk,
		event.TypeThoughtChunk,
		event.TypeAvailableCommands,
		event.TypeUsage,
	}
	for _, tc := range droppable {
		t.Run(string(tc), func(t *testing.T) {
			if event.IsControl(tc) {
				t.Fatalf("IsControl(%q) = true, want false", tc)
			}
		})
	}
}

func TestIsControlUnknownType(t *testing.T) {
	if event.IsControl("unknown_event_type") {
		t.Fatal("IsControl('unknown_event_type') = true, want false")
	}
	if event.IsControl("") {
		t.Fatal("IsControl('') = true, want false")
	}
}

func TestPlanConstants(t *testing.T) {
	if event.PlanStatusPending != "pending" {
		t.Fatalf("PlanStatusPending = %q, want %q", event.PlanStatusPending, "pending")
	}
	if event.PlanStatusInProgress != "in_progress" {
		t.Fatalf("PlanStatusInProgress = %q, want %q", event.PlanStatusInProgress, "in_progress")
	}
	if event.PlanStatusCompleted != "completed" {
		t.Fatalf("PlanStatusCompleted = %q, want %q", event.PlanStatusCompleted, "completed")
	}
	if event.PlanPriorityHigh != "high" {
		t.Fatalf("PlanPriorityHigh = %q, want %q", event.PlanPriorityHigh, "high")
	}
	if event.PlanPriorityMedium != "medium" {
		t.Fatalf("PlanPriorityMedium = %q, want %q", event.PlanPriorityMedium, "medium")
	}
	if event.PlanPriorityLow != "low" {
		t.Fatalf("PlanPriorityLow = %q, want %q", event.PlanPriorityLow, "low")
	}
}

func TestPermissionStatusConstants(t *testing.T) {
	if event.PermissionStatusResolved != "resolved" {
		t.Fatalf("PermissionStatusResolved = %q, want %q", event.PermissionStatusResolved, "resolved")
	}
	if event.PermissionStatusCancelled != "cancelled" {
		t.Fatalf("PermissionStatusCancelled = %q, want %q", event.PermissionStatusCancelled, "cancelled")
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	in := event.Event{
		Type:         event.TypeSessionStatus,
		SessionID:    "ses_abc123",
		Timestamp:    ts,
		Seq:          42,
		Status:       "running",
		Text:         "Session is active",
		PermissionID: "perm_1",
		Options: []event.PermissionOption{
			{OptionID: "allow", Name: "Allow", Kind: "once"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out event.Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type {
		t.Fatalf("Type = %q, want %q", out.Type, in.Type)
	}
	if out.SessionID != in.SessionID {
		t.Fatalf("SessionID = %q, want %q", out.SessionID, in.SessionID)
	}
	if !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("Timestamp = %v, want %v", out.Timestamp, in.Timestamp)
	}
	if out.Seq != in.Seq {
		t.Fatalf("Seq = %d, want %d", out.Seq, in.Seq)
	}
	if out.Status != in.Status {
		t.Fatalf("Status = %q, want %q", out.Status, in.Status)
	}
	if len(out.Options) != 1 || out.Options[0].OptionID != "allow" {
		t.Fatalf("Options = %+v", out.Options)
	}
}

func TestEventZeroValuesOmitted(t *testing.T) {
	e := event.Event{
		Type:      event.TypeError,
		SessionID: "s1",
		Timestamp: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["seq"]; ok {
		t.Fatal("zero Seq should be omitted from JSON")
	}
	if _, ok := raw["text"]; ok {
		t.Fatal("empty Text should be omitted from JSON")
	}
	if _, ok := raw["status"]; ok {
		t.Fatal("empty Status should be omitted from JSON")
	}
	if _, ok := raw["replay"]; ok {
		t.Fatal("false Replay should be omitted from JSON")
	}
}

func TestCapabilitiesJSON(t *testing.T) {
	in := event.Capabilities{
		Image:       true,
		Audio:       false,
		LoadSession: true,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out event.Capabilities
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Image != true || out.Audio != false || out.LoadSession != true {
		t.Fatalf("Capabilities = %+v", out)
	}
}

func TestConfigOptionJSON(t *testing.T) {
	in := event.ConfigOption{
		ID:           "model",
		Name:         "Model",
		Kind:         "select",
		CurrentValue: "gpt-4o",
		Values: []event.ConfigOptionValue{
			{ID: "gpt-4o", Name: "GPT-4o"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out event.ConfigOption
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != in.ID || out.Kind != in.Kind || out.CurrentValue != in.CurrentValue {
		t.Fatalf("got %+v, want %+v", out, in)
	}
	if len(out.Values) != 1 || out.Values[0].ID != "gpt-4o" {
		t.Fatalf("Values = %+v", out.Values)
	}
}

func TestAttachmentInfoJSON(t *testing.T) {
	in := event.AttachmentInfo{
		Kind:     "image",
		MimeType: "image/png",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out event.AttachmentInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Kind != "image" || out.MimeType != "image/png" {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}
