//go:build live_grok

package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Run with: go test -tags live_grok ./internal/session/ -run TestLivePlan -count=1
//
// End-to-end: the daemon's /plan command against a real grok agent — the one
// path unit tests cannot cover, since it depends on grok honoring
// session/set_mode and echoing the result back as current_mode_update
// (MADR 0022).
func TestLivePlanCommandEndToEnd(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	reg := provider.NewRegistry()
	reg.Register(p)

	var sink eventSink
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	meta, err := mgr.Create(ctx, provider.IDGrok,
		provider.StartOptions{Name: "plan-e2e", CWD: t.TempDir()}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer mgr.Close(context.Background(), meta.ID, "dev-a")

	sink.waitForMode(t, "default")

	if err := mgr.Prompt(ctx, meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("/plan: %v", err)
	}
	sink.waitForMode(t, "plan") // grok's own current_mode_update
	if !sink.hasNoticeContaining("Plan mode on") {
		t.Fatalf("notices: %v", sink.notices())
	}

	if err := mgr.Prompt(ctx, meta.ID, "/plan off", nil, "dev-a"); err != nil {
		t.Fatalf("/plan off: %v", err)
	}
	sink.waitForNoticeContaining(t, "Plan mode off")

	if err := mgr.Prompt(ctx, meta.ID, "/mode", nil, "dev-a"); err != nil {
		t.Fatalf("/mode: %v", err)
	}
	if !sink.hasNoticeContaining("Modes: default") {
		t.Fatalf("notices: %v", sink.notices())
	}
	for _, ev := range sink.snapshot() {
		if ev.Type == event.TypeNotice {
			t.Logf("notice: %s", ev.Text)
		}
	}
}
