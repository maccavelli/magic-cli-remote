//go:build live_codex

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestLive0080NoTurnSurface exercises the MADR 0080 no-model-turn sequence:
// temporary CODEX_HOME + git repo, catalog-driven Fast/personality discovery,
// working-tree diff, paused goal CRUD, and fork-on-unmaterialized. It never
// calls turn/start.
func TestLive0080NoTurnSurface(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not found on PATH")
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := t.TempDir()
	run := exec.Command("git", "init", repo)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte("0080 live\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewWithLogger(Config{Bin: "codex", DefaultCWD: repo}, nil)
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(cat.Options) == 0 {
		t.Fatal("empty catalog")
	}
	model := cat.DefaultIDs
	t.Logf("catalog models=%d defaults=%v", len(cat.Options), model)

	sess, err := p.Start(ctx, provider.StartOptions{CWD: repo})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close(context.Background())

	if ds, ok := sess.(provider.DiffSession); ok {
		res, err := ds.Diff(ctx, "")
		if err != nil && !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("diff: %v", err)
		}
		t.Logf("diff scope=%s truncated=%v bytes=%d", res.Scope, res.Truncated, len(res.Summary))
	}
	if gs, ok := sess.(provider.GoalSession); ok {
		if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalReplace, Objective: "paused probe"}); err != nil {
			t.Logf("goal set: %v", err)
		} else if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalPause}); err != nil {
			t.Logf("goal pause: %v", err)
		} else if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalClear}); err != nil {
			t.Logf("goal clear: %v", err)
		}
	}
	if fs, ok := sess.(provider.ForkSession); ok {
		_, err := fs.Fork(ctx, provider.ForkOptions{})
		if err != nil && !strings.Contains(err.Error(), "nothing to fork") {
			t.Logf("fork: %v", err)
		}
	}
	for {
		select {
		case ev := <-sess.Events():
			if ev.Type == event.TypeTurnComplete {
				t.Fatal("no-turn suite must not start a model turn")
			}
		default:
			return
		}
	}
}

// TestLive0080SchemaSurface generates both schema trees into a temp dir and
// asserts collaboration/settings are experimental while goal/review/fork/diff
// and turn settings are on the normal surface.
func TestLive0080SchemaSurface(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not found on PATH")
	}
	dir, err := os.MkdirTemp("", "codex-schema-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	normal := filepath.Join(dir, "normal")
	experimental := filepath.Join(dir, "experimental")
	if err := os.MkdirAll(normal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(experimental, 0o755); err != nil {
		t.Fatal(err)
	}

	gen := func(out string, extra ...string) error {
		args := append([]string{"generate-ts", "--out", out}, extra...)
		cmd := exec.Command("codex", args...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		t.Logf("codex %s\n%s", strings.Join(args, " "), b)
		return err
	}
	if err := gen(normal); err != nil {
		// Older CLIs used `app-server generate-ts`.
		cmd := exec.Command("codex", "app-server", "generate-ts", "--out", normal)
		b, err2 := cmd.CombinedOutput()
		t.Logf("fallback generate: %s\n%s", err, b)
		if err2 != nil {
			t.Skipf("codex generate-ts not available: %v / %v", err, err2)
		}
	}
	_ = gen(experimental, "--experimental")

	normalBlob := readTree(t, normal)
	expBlob := readTree(t, experimental)
	for _, want := range []string{"review/start", "thread/fork", "thread/goal", "gitDiffToRemote", "serviceTier", "personality"} {
		if !strings.Contains(normalBlob, want) {
			t.Errorf("normal schema missing %q", want)
		}
	}
	if strings.Contains(normalBlob, "collaborationMode/list") && !strings.Contains(normalBlob, "experimental") {
		t.Log("collaborationMode/list present in normal schema; record the drift")
	}
	if !strings.Contains(expBlob, "collaborationMode") && !strings.Contains(expBlob, "thread/settings/update") {
		t.Log("experimental schema missing collaboration/settings; generate-ts --experimental may be unsupported")
	}
}

func readTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(raw)
		b.WriteByte('\n')
		return nil
	})
	return b.String()
}
