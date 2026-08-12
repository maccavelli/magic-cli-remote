package codex

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestParseGoalMutation(t *testing.T) {
	view, err := parseGoalMutation("")
	if err != nil || view.Kind != provider.GoalView {
		t.Fatalf("view = %+v err=%v", view, err)
	}
	rep, err := parseGoalMutation("ship it")
	if err != nil || rep.Kind != provider.GoalReplace || rep.Objective != "ship it" {
		t.Fatalf("replace = %+v err=%v", rep, err)
	}
	edit, err := parseGoalMutation("edit  keep going")
	if err != nil || edit.Kind != provider.GoalEdit || edit.Objective != "keep going" {
		t.Fatalf("edit = %+v err=%v", edit, err)
	}
	if _, err := parseGoalMutation("pause extra"); err == nil {
		t.Fatal("pause extra must fail")
	}
	ok := strings.Repeat("é", 4000)
	if utf8.RuneCountInString(ok) != 4000 {
		t.Fatal("fixture")
	}
	if _, err := parseGoalMutation(ok); err != nil {
		t.Fatal(err)
	}
	if _, err := parseGoalMutation(ok + "x"); err == nil {
		t.Fatal("4001 scalars must fail")
	}
	if _, err := parseGoalMutation("edit   "); err == nil {
		t.Fatal("empty edit objective must fail")
	}
}

func TestGoalPlanMatrix(t *testing.T) {
	active := provider.Goal{Status: provider.GoalStatusActive, Objective: "x"}
	paused := provider.Goal{Status: provider.GoalStatusPaused, Objective: "x"}
	cases := []struct {
		name    string
		mut     provider.GoalMutation
		cur     provider.Goal
		present bool
		inPlan  bool
		wantErr bool
	}{
		{"view in plan", provider.GoalMutation{Kind: provider.GoalView}, active, true, true, false},
		{"pause in plan", provider.GoalMutation{Kind: provider.GoalPause}, active, true, true, false},
		{"clear in plan", provider.GoalMutation{Kind: provider.GoalClear}, active, true, true, false},
		{"edit paused in plan", provider.GoalMutation{Kind: provider.GoalEdit, Objective: "y"}, paused, true, true, false},
		{"edit active in plan", provider.GoalMutation{Kind: provider.GoalEdit, Objective: "y"}, active, true, true, true},
		{"replace in plan", provider.GoalMutation{Kind: provider.GoalReplace, Objective: "y"}, provider.Goal{}, false, true, true},
		{"resume in plan", provider.GoalMutation{Kind: provider.GoalResume}, paused, true, true, true},
		{"replace default", provider.GoalMutation{Kind: provider.GoalReplace, Objective: "y"}, provider.Goal{}, false, false, false},
	}
	for _, tc := range cases {
		err := checkGoalPlanMatrix(tc.mut, tc.cur, tc.present, tc.inPlan)
		if tc.wantErr && err == nil {
			t.Fatalf("%s: want error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

func TestGoalBusySkipsRPC(t *testing.T) {
	s := seededCollabSession(t)
	s.turnBusy = true
	s.p = &Provider{log: testLogger(t)}
	_, err := s.ApplyGoal(context.Background(), provider.GoalMutation{Kind: provider.GoalReplace, Objective: "x"})
	if !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("err = %v", err)
	}
	_, err = s.ApplyGoal(context.Background(), provider.GoalMutation{Kind: provider.GoalView})
	if err != nil {
		t.Fatalf("view must work while busy: %v", err)
	}
}

func TestGoalDecodeStatusesAndAdditive(t *testing.T) {
	raw := []byte(`{"goal":{"objective":"do it","status":"usageLimited","tokenBudget":10,"tokenUsage":3,"future":true}}`)
	g, ok := decodeGoalResult(raw, false)
	if !ok || g.Status != "usageLimited" || g.TokenBudget != 10 || g.TokenUsage != 3 {
		t.Fatalf("%+v ok=%v", g, ok)
	}
	if _, ok := decodeGoalGet([]byte("null")); ok {
		t.Fatal("null get is absence")
	}
	if _, ok := decodeGoalResult([]byte(`{"cleared":true}`), true); ok {
		t.Fatal("cleared")
	}
}

func TestGoalLogsOmitObjective(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := seededCollabSession(t)
	s.log = log
	s.rememberGoal(provider.Goal{Objective: "SECRET-OBJECTIVE", Status: "active"}, true)
	s.emitGoal()
	if strings.Contains(buf.String(), "SECRET-OBJECTIVE") {
		t.Fatalf("objective leaked into logs: %s", buf.String())
	}
}

func TestPlanEntryRejectedWhenGoalActive(t *testing.T) {
	s := seededCollabSession(t)
	s.rememberGoal(provider.Goal{Objective: "x", Status: provider.GoalStatusActive}, true)
	if err := s.SetCollaborationMode(context.Background(), "plan"); !errors.Is(err, provider.ErrGoalPlanConflict) {
		t.Fatalf("err = %v", err)
	}
}
