//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run MCPNotifications -count=1
//
// Pins D5: verifies session initialization and MCP status tracking capability.
func TestLiveGrokMCPStatusSessionInterface(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "mcp-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	mcpSession, ok := s.(provider.MCPStatusSession)
	if !ok {
		t.Fatal("grok session must implement provider.MCPStatusSession")
	}

	statuses, err := mcpSession.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("MCPStatus error: %v", err)
	}
	t.Logf("MCPStatus returned %d server statuses", len(statuses))
}
