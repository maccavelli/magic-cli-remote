//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run ToolPolicy -count=1
//
// Pins why providers.grok has no allowed_tools, disallowed_tools, allow_rules
// or deny_rules (0208 PLAN P8, grok commit). Each was passed as its grok flag
// and measured with no effect on a session driven over ACP: on grok 0.2.114
// (2026-07-29) and again on 1.0.70 (2026-11-04), asking the agent to write a
// file. Magic no longer passes them.
//
// Each case passes its flag by hand, in the argv Magic would build, and fails
// if grok now honours it: the four keys and their flags should then come back.
func TestLiveGrokToolPolicyFlagsStillHaveNoEffect(t *testing.T) {
	base := []string{"--permission-mode", "default"}
	tail := []string{"--no-auto-update", "agent", "--no-leader", "stdio"}
	argv := func(flag ...string) []string {
		return append(append(append([]string{}, base...), flag...), tail...)
	}
	cases := []struct {
		name, key string
		cfg       grok.Config
		// writes: the agent, auto-approved, still writes the file. Otherwise
		// the agent, prompting, still asks before writing.
		writes bool
	}{
		{"disallowed-tools", "disallowed_tools",
			grok.Config{AlwaysApprove: true, Args: argv("--disallowed-tools", "Write")}, true},
		{"tools", "allowed_tools", grok.Config{AlwaysApprove: true, Args: argv("--tools", "Read")}, true},
		{"deny", "deny_rules", grok.Config{AlwaysApprove: true, Args: argv("--deny", "Write")}, true},
		{"allow", "allow_rules", grok.Config{Args: argv("--allow", "Write")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := grok.New(c.cfg)
			if !p.Ready() {
				t.Skip("grok not in PATH")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			dir := t.TempDir()
			s, err := p.Start(ctx, provider.StartOptions{Name: "toolpolicy-" + c.name, CWD: dir})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer s.Close(context.Background())
			if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Create a file named probe.txt " +
				"containing OK in the current directory. Use your file-writing tool, not the shell. Do it now, then stop."}}); err != nil {
				t.Fatalf("prompt: %v", err)
			}
			prompts := 0
			deadline := time.Now().Add(2 * time.Minute)
			for done := false; !done && time.Now().Before(deadline); {
				select {
				case ev, ok := <-s.Events():
					if !ok {
						done = true
						break
					}
					switch ev.Type {
					case event.TypePermission:
						prompts++
						// Refuse, so a prompting case never writes.
						for _, o := range ev.Options {
							if o.Kind == "reject_once" {
								_ = s.(provider.PermissionSession).RespondPermission(ctx, ev.PermissionID, o.OptionID, false, "")
							}
						}
					case event.TypeTurnComplete:
						done = true
					}
				case <-time.After(300 * time.Millisecond):
				}
			}
			files, err := workspaceFiles(dir)
			if err != nil {
				t.Fatalf("list workspace: %v", err)
			}
			t.Logf("%s: prompts=%d files=%q", c.name, prompts, files)
			if c.writes && len(files) == 0 {
				t.Errorf("no file written under --%s: grok may now honour it over ACP; "+
					"measure again, and restore providers.grok.%s if it binds", c.name, c.key)
			}
			if !c.writes && prompts == 0 {
				t.Errorf("no prompt under --%s: grok may now honour it over ACP; "+
					"measure again, and restore providers.grok.%s if it binds", c.name, c.key)
			}
		})
	}
}
