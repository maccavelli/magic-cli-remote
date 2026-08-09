//go:build live_grok

package grok_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with:
//
//	go test -tags live_grok ./internal/provider/grok/ -run PlanApproval -count=1 -timeout 10m
//
// Pins the whole plan-approval hand-off (MADR 0022 phase 2) against the real
// shell, because every part of it is undocumented and was established by probing:
//
//   - grok asks the *client* to approve, over the extension request
//     _x.ai/exit_plan_mode, and fails the tool call if the client does not answer
//     it. That is why we implement it at all.
//   - the request carries the plan markdown as planContent, which is what makes a
//     phone-side approval card possible.
//   - answering {"outcome":"approved"} is what its shell reads as "leave plan mode
//     and start implementing". Any value it does not recognize means "revise" —
//     silently, with no error — so a rename on grok's side would otherwise look
//     like the user endlessly asking for changes. This test is what notices.
func TestLiveGrokPlanApprovalHandOff(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	start := time.Now()

	s, err := p.Start(ctx, provider.StartOptions{Name: "plan-approval-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatal("grok session must implement provider.ModeSession")
	}
	if err := ms.SetMode(ctx, "plan"); err != nil {
		t.Fatalf("set plan mode: %v", err)
	}
	ps, ok := s.(provider.PermissionSession)
	if !ok {
		t.Fatal("grok session must implement provider.PermissionSession")
	}

	go func() {
		err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "" +
			"Write a one-sentence plan for adding a greet(name) function to hello.py, " +
			"then call exit_plan_mode to present it for approval. Do not write code."}})
		if err != nil {
			t.Logf("prompt returned: %v", err)
		}
	}()

	// Phase 1: the approval arrives as an ordinary permission_request, so the
	// existing phone UI can answer it with no client change.
	var approval event.Event
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) && approval.PermissionID == "" {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypePermission && ev.ToolName == "Plan ready for review" {
				approval = ev
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if approval.PermissionID == "" {
		t.Fatal("grok never asked the client to approve the plan")
	}
	if strings.TrimSpace(approval.Text) == "" {
		t.Fatal("approval carried no plan text")
	}
	var hasApprove bool
	for _, o := range approval.Options {
		if o.OptionID == "plan_approve" {
			hasApprove = true
		}
	}
	if !hasApprove {
		t.Fatalf("approval options = %+v, want an approve option", approval.Options)
	}
	t.Logf("plan approval after %s, plan text %d bytes: %.120q",
		time.Since(start).Round(time.Millisecond), len(approval.Text), approval.Text)

	if err := ps.RespondPermission(ctx, approval.PermissionID, "plan_approve", false, "dev-1"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	// Phase 2: grok must read the answer as an approval, which shows up as the
	// session leaving plan mode ("Active → Inactive (exit_plan_mode approved)" in
	// its own state machine) and the tool call retitled "Plan mode exited".
	//
	// The negative assertion matters more than the positive one: an outcome grok
	// does not recognize is reported as "the user wants to revise the plan" with
	// no error anywhere, so a rename on its side would look like a working
	// feature that quietly never approves anything.
	//
	// Note the confirmation is NOT in the tool result *text* — grok puts the plan
	// file path there and the "your plan has been approved" wording only in
	// rawOutput, which our summarizer skips whenever structured content exists.
	deadline = time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if strings.Contains(strings.ToLower(ev.Text), "revise the plan") {
				t.Fatalf("grok read our approval as a revision request: %q", ev.Text)
			}
			if ev.Type == event.TypeMode && ev.CurrentModeID != "" && ev.CurrentModeID != "plan" {
				t.Logf("left plan mode (now %q) %s after start", ev.CurrentModeID,
					time.Since(start).Round(time.Millisecond))
				return
			}
			if ev.Type == event.TypeToolUpdate && ev.ToolName == "Plan mode exited" {
				t.Logf("tool retitled %q %s after start", ev.ToolName,
					time.Since(start).Round(time.Millisecond))
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("grok never left plan mode after the approval")
}
