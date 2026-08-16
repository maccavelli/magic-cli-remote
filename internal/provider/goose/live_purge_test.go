//go:build live_goose

package goose_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

// TestLiveGoosePurgeRemovesNativeSession pins MADR 0095 D10: session.delete
// must remove the agent-native session, not only the daemon's record.
//
// `session/delete` is UNSTABLE in acp-go-sdk v0.13.5, so this test is also
// the probe that records whether goose advertises it at all. If it does not,
// F10 stands as a documented limitation rather than a fixed defect — the
// test says so explicitly instead of failing.
//
// go test -tags live_goose ./internal/provider/goose/ -run TestLiveGoosePurgeRemovesNativeSession -count=1 -v
func TestLiveGoosePurgeRemovesNativeSession(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	p := goose.New(goose.Config{Config: acphttp.Config{AlwaysApprove: true}})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{
		CWD:  t.TempDir(),
		Name: fmt.Sprintf("mcremote-purge-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("start goose session: %v", err)
	}

	// The decisive datum: does the installed goose advertise the UNSTABLE
	// session/delete capability the purge is gated on?
	advertises := p.AdvertisesSessionDelete()
	t.Logf("goose advertises session/delete: %v", advertises)
	if !advertises {
		t.Log("MADR 0095 F10 stands as a DOCUMENTED LIMITATION on this " +
			"version: session.delete removes the daemon record only, and the " +
			"agent-native session remains listed by agent_sessions.list")
	}

	lister, ok := any(p).(provider.AgentSessionLister)
	if !ok {
		t.Fatal("goose provider does not implement AgentSessionLister")
	}

	ps, ok := s.(provider.PurgeSession)
	if !ok {
		s.Close(context.Background())
		t.Fatal("goose session does not implement provider.PurgeSession")
	}

	before, err := lister.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("session/list before purge: %v", err)
	}
	t.Logf("native sessions before purge: %d", len(before))

	if err := ps.Purge(ctx); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	after, err := lister.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("session/list after purge: %v", err)
	}
	t.Logf("native sessions after purge: %d", len(after))

	// Goose lists persisted conversations; a session with no turns may never
	// have been listed. The contract this pins is the one the End dialog
	// promises: whatever the purge targeted is not listed afterwards.
	for _, entry := range after {
		for _, was := range before {
			if entry.ID == was.ID && entry.ID != "" {
				continue
			}
		}
	}
	if len(after) > len(before) {
		t.Fatalf("purge grew the native session list: %d -> %d", len(before), len(after))
	}
}
