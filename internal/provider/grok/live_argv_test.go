//go:build live_grok

package grok_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run Argv -count=1
//
// Pins the argument vector against the installed grok. This is the test whose
// absence let MADR 0050 ship: every one of these flags was placed after the
// `agent` subcommand, where grok rejects it with
//
//	error: unexpected argument '--permission-mode' found
//
// so seven config options killed session start — and the unit tests all passed,
// because they assert the argv *we build* rather than what grok accepts.
//
// Driving provider.Start (not exec.Command) is deliberate: it exercises the
// real spawn path including --no-leader and the ACP initialize handshake, so a
// flag grok parses but rejects semantically also shows up here.
//
// When grok next relocates a flag, this fails. Re-pinned to grok 1.0.5
// (5115b46bc909) [stable] (MADR 0106). Placement is the contract; -m and
// --reasoning-effort still parse and still do not apply over ACP.
func TestLiveGrokArgvAcceptsEveryConfiguredFlag(t *testing.T) {
	cases := []struct {
		name string
		cfg  grok.Config
	}{
		{"bare", grok.Config{}},
		{"model", grok.Config{Model: "grok-4.5"}},
		{"model46", grok.Config{Model: "grok-4.6"}},
		{"reasoningEffort", grok.Config{ReasoningEffort: "low"}},
		{"reasoningXHigh", grok.Config{ReasoningEffort: "xhigh"}},
		{"alwaysApprove", grok.Config{AlwaysApprove: true}},
		{"permissionMode", grok.Config{PermissionMode: "default"}},
		{"allowedTools", grok.Config{AllowedTools: []string{"Bash"}}},
		{"disallowedTools", grok.Config{DisallowedTools: []string{"Bash"}}},
		{"allowRules", grok.Config{AllowRules: []string{"Bash"}}},
		{"denyRules", grok.Config{DenyRules: []string{"Bash"}}},
		{"noSubagents", grok.Config{NoSubagents: true}},
		{"disableWebSearch", grok.Config{DisableWebSearch: true}},
		{"sandboxOff", grok.Config{Sandbox: "off"}},
		{"sandboxWorkspace", grok.Config{Sandbox: "workspace"}},
		{"sandboxReadOnly", grok.Config{Sandbox: "read-only"}},
		{"everything", grok.Config{
			Model:            "grok-4.6",
			ReasoningEffort:  "xhigh",
			PermissionMode:   "default",
			AllowedTools:     []string{"Bash"},
			DisallowedTools:  []string{"Write"},
			AllowRules:       []string{"Bash"},
			DenyRules:        []string{"Write"},
			NoSubagents:      true,
			DisableWebSearch: true,
			Sandbox:          "workspace",
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := grok.New(c.cfg)
			if !p.Ready() {
				t.Skip("grok not in PATH")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			s, err := p.Start(ctx, provider.StartOptions{
				Name: "argv-" + c.name,
				CWD:  t.TempDir(),
			})
			if err != nil {
				// The failure this pins: grok refusing the vector outright.
				if strings.Contains(err.Error(), "unexpected argument") {
					t.Fatalf("grok rejected the argument vector — a flag has moved "+
						"or is misplaced: %v", err)
				}
				t.Fatalf("start: %v", err)
			}
			_ = s.Close(context.Background())
		})
	}
}
