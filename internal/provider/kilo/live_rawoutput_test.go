//go:build live_kilo

package kilo_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// TestLiveRawOutputSamples records what Magic receives when kilo runs commands
// whose output has layout: a listing, a diff, JSON, colour, box drawing and an
// aligned table (PLAN 0208 P3.0). It does nothing unless
// MCREMOTE_RAW_OUTPUT_DIR is set, so a routine `make live-kilo` spends nothing
// on it.
//
//	MCREMOTE_RAW_OUTPUT_DIR=<dir> go test -tags live_kilo ./internal/provider/kilo/ \
//	  -run TestLiveRawOutputSamples -count=1 -timeout 600s -v
func TestLiveRawOutputSamples(t *testing.T) {
	out := os.Getenv("MCREMOTE_RAW_OUTPUT_DIR")
	if out == "" {
		t.Skip("MCREMOTE_RAW_OUTPUT_DIR not set")
	}
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: true, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "raw-output", CWD: rawOutputWorkspace(t)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	writeRawOutput(t, out, "kilo", collectRawOutput(ctx, t, s))
}

// rawOutputPrompt asks for one tool call per command, then a Markdown reply.
const rawOutputPrompt = "Run each of these shell commands one at a time, each as its own tool call, " +
	"exactly as written, and do not combine them:\n" +
	"1. ls -la\n2. git --no-pager diff\n3. cat data.json\n4. sh show.sh\n5. sh table.sh\n" +
	"Then reply with a short Markdown summary: a heading, a bullet list, and a table of the " +
	"commands and what each printed."

// rawOutputSample is one thing Magic received: a tool event's text and diffs,
// or the assistant's reply.
type rawOutputSample struct {
	Kind  string           `json:"kind"`
	Name  string           `json:"name,omitempty"`
	Text  string           `json:"text"`
	Diffs []event.ToolDiff `json:"diffs,omitempty"`
}

// rawOutputWorkspace is a git repository with one modified file, a JSON file,
// a script printing colour and box drawing, and one printing an aligned table.
func rawOutputWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=fixture",
			"-c", "user.email=fixture@example.invalid"}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	git("init", "-q")
	git("add", "main.go")
	git("commit", "-q", "-m", "initial")
	write("main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello, world\")\n}\n")
	write("data.json", "{\n  \"name\": \"alpha\",\n  \"replicas\": 3,\n  \"ports\": [80, 443],\n"+
		"  \"labels\": {\"tier\": \"web\", \"env\": \"staging\"}\n}\n")
	write("show.sh", "printf '\\033[1;32mPASS\\033[0m  pkg/alpha  0.012s\\n'\n"+
		"printf '\\033[1;31mFAIL\\033[0m  pkg/beta   0.034s\\n'\n"+
		"printf '┌──────────┬────────┐\\n│ name     │ status │\\n├──────────┼────────┤\\n'\n"+
		"printf '│ alpha    │ ok     │\\n│ beta     │ failed │\\n└──────────┴────────┘\\n'\n")
	write("table.sh", "printf '%-10s %-9s %s\\n' NAME STATUS AGE api Running 3d worker Pending 12m cron Failed 1h\n")
	return dir
}

// collectRawOutput sends the prompt and keeps each tool's last text and
// diffs, in first-seen order, and the assistant's whole reply.
func collectRawOutput(ctx context.Context, t *testing.T, s provider.Session) []rawOutputSample {
	t.Helper()
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: rawOutputPrompt}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	var order []string
	tools := map[string]rawOutputSample{}
	reply := ""
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("turn did not complete: %v", ctx.Err())
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed before the turn completed")
			}
			switch ev.Type {
			case event.TypeToolCall, event.TypeToolUpdate:
				prev, seen := tools[ev.ToolID]
				if !seen {
					order = append(order, ev.ToolID)
				}
				if ev.ToolName != "" && ev.ToolName != "tool" {
					prev.Name = ev.ToolName
				}
				if ev.Text != "" {
					prev.Text = ev.Text
				}
				if len(ev.Diffs) > 0 {
					prev.Diffs = ev.Diffs
				}
				prev.Kind = "tool"
				tools[ev.ToolID] = prev
			case event.TypeAssistantChunk:
				reply += ev.Text
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			case event.TypeTurnComplete:
				var out []rawOutputSample
				for _, id := range order {
					out = append(out, tools[id])
				}
				return append(out, rawOutputSample{Kind: "assistant", Text: reply})
			}
		}
	}
}

// writeRawOutput writes the samples to <dir>/<provider>/samples.json.
func writeRawOutput(t *testing.T, dir, name string, samples []rawOutputSample) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name, "samples.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d samples to %s", len(samples), path)
}
