package protocol

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestQuestionRespondPayloadKeyedAndLegacyCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"keyed", `{"session_id":"s","question_id":"q","answers":{"field":["yes"]}}`},
		{"legacy ordered", `{"session_id":"s","question_id":"q","answers":[["yes"],["no"]]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload QuestionRespondPayload
			if err := json.Unmarshal([]byte(tc.raw), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.SessionID != "s" || payload.QuestionID != "q" || len(payload.Answers) == 0 {
				t.Fatalf("payload = %+v", payload)
			}
		})
	}
}

// TestQuestionRespondPayloadDeclined is PLAN 0191 L1 step 1: declined is an
// additive answer, omitted unless set, and logged (MADR 0191 D3, D4).
func TestQuestionRespondPayloadDeclined(t *testing.T) {
	var p QuestionRespondPayload
	if err := json.Unmarshal([]byte(`{"session_id":"s","question_id":"q","declined":true}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Declined || p.Cancelled {
		t.Fatalf("payload = %+v, want declined and not cancelled", p)
	}
	raw, err := json.Marshal(QuestionRespondPayload{SessionID: "s", QuestionID: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "declined") {
		t.Fatalf("declined sent when false: %s", raw)
	}
	var logs strings.Builder
	slog.New(slog.NewTextHandler(&logs, nil)).Info("q", slog.Any("payload", p))
	if !strings.Contains(logs.String(), "declined=true") {
		t.Fatalf("declined missing from the log line: %s", logs.String())
	}
}

func TestQuestionRespondPayloadRedactsAndClearsAnswers(t *testing.T) {
	const secret = "P2-PROTOCOL-SECRET-SENTINEL"
	payload := QuestionRespondPayload{
		SessionID: "s", QuestionID: "q",
		Answers: map[string][]string{"secret": {secret}},
	}
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("question", slog.Any("payload", payload), slog.Any("answers", payload.Answers))
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("secret reached structured logs: %s", logs.String())
	}
	payload.ClearAnswers()
	if payload.Answers["secret"] != nil {
		t.Fatalf("answer was not cleared: %#v", payload.Answers)
	}
}
