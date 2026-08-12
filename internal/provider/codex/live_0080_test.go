//go:build live_codex

package codex

import (
	"context"
	"errors"
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
// temporary CODEX_HOME + git repo with an origin remote (gitDiffToRemote
// requires a remote-tracking SHA), catalog-driven Fast/personality, Plan
// then Default, paused goal CRUD, working-tree diff, and fork after the
// goal materializes a rollout. It never calls turn/start.
func TestLive0080NoTurnSurface(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not found on PATH")
	}
	if ver, err := exec.Command("codex", "--version").CombinedOutput(); err == nil {
		t.Logf("codex --version: %s", strings.TrimSpace(string(ver)))
	}

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := t.TempDir()
	remote := t.TempDir()

	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=0080", "GIT_AUTHOR_EMAIL=0080@example.test",
			"GIT_COMMITTER_NAME=0080", "GIT_COMMITTER_EMAIL=0080@example.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git(remote, "init", "--bare", "--initial-branch=main")
	git(repo, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "tracked.txt")
	git(repo, "commit", "-m", "initial")
	git(repo, "remote", "add", "origin", remote)
	git(repo, "-c", "protocol.file.allow=always", "push", "-u", "origin", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("tracked changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked 0080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	headOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	head := strings.TrimSpace(string(headOut))

	p := NewWithLogger(Config{Bin: "codex", DefaultCWD: repo}, nil)
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(cat.Options) == 0 {
		t.Fatal("empty catalog")
	}
	t.Logf("catalog models=%d defaults=%v", len(cat.Options), cat.DefaultIDs)

	sess, err := p.Start(ctx, provider.StartOptions{CWD: repo})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close(context.Background())

	if cs, ok := sess.(provider.CollaborationModeSession); ok {
		modes, current, err := cs.CollaborationModes()
		if err != nil {
			t.Fatalf("collaboration catalog: %v", err)
		}
		t.Logf("collaboration current=%s modes=%v", current, modes)
		var havePlan, haveDefault bool
		for _, m := range modes {
			switch strings.ToLower(m.ID) {
			case "plan":
				havePlan = true
			case "default":
				haveDefault = true
			}
		}
		if !havePlan || !haveDefault {
			t.Fatalf("collaboration catalog missing Plan/Default: %v", modes)
		}
		if err := cs.SetCollaborationMode(ctx, "plan"); err != nil {
			t.Fatalf("set plan: %v", err)
		}
		if _, now, err := cs.CollaborationModes(); err != nil || !strings.EqualFold(now, "plan") {
			t.Fatalf("after plan: current=%q err=%v", now, err)
		}
		if err := cs.SetCollaborationMode(ctx, "default"); err != nil {
			t.Fatalf("set default: %v", err)
		}
	} else {
		t.Fatal("session does not implement CollaborationModeSession")
	}

	if ts, ok := sess.(provider.ServiceTierSession); ok && ts.HasFast() {
		if err := ts.SetServiceTier(ctx, true); err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
			t.Fatalf("fast on: %v", err)
		}
		if err := ts.SetServiceTier(ctx, false); err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
			t.Fatalf("fast off: %v", err)
		}
	} else {
		t.Log("active model has no Fast tier; skipped")
	}
	if ps, ok := sess.(provider.PersonalitySession); ok && ps.PersonalitySupported() {
		if err := ps.SetPersonality(ctx, "friendly"); err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
			t.Fatalf("personality friendly: %v", err)
		}
		if err := ps.SetPersonality(ctx, "none"); err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
			t.Fatalf("personality none: %v", err)
		}
	} else {
		t.Log("active model has no personality; skipped")
	}

	if gs, ok := sess.(provider.GoalSession); ok {
		if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalReplace, Objective: "paused probe"}); err != nil {
			t.Fatalf("goal set: %v", err)
		}
		if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalPause}); err != nil {
			t.Fatalf("goal pause: %v", err)
		}
		if g, ok := gs.CurrentGoal(); !ok || g.Objective == "" {
			t.Fatalf("goal get after pause: present=%v %#v", ok, g)
		}
		if _, err := gs.ApplyGoal(ctx, provider.GoalMutation{Kind: provider.GoalClear}); err != nil {
			t.Fatalf("goal clear: %v", err)
		}
	} else {
		t.Fatal("session does not implement GoalSession")
	}

	ds, ok := sess.(provider.DiffSession)
	if !ok {
		t.Fatal("session does not implement DiffSession")
	}
	res, err := ds.Diff(ctx, "")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if res.BaseSHA != "" && !strings.EqualFold(res.BaseSHA, head) {
		t.Errorf("diff sha=%s want HEAD=%s", res.BaseSHA, head)
	}
	if !strings.Contains(res.Summary, "tracked.txt") {
		t.Errorf("diff missing tracked.txt: %q", clip(res.Summary, 300))
	}
	if !strings.Contains(res.Summary, "untracked.txt") {
		t.Errorf("diff missing untracked.txt (measured 0.147 includes untracked): %q", clip(res.Summary, 300))
	}
	if strings.Contains(res.Summary, "/Users/") && !strings.Contains(res.Summary, repo) {
		t.Errorf("diff leaked a path outside the temp repo: %q", clip(res.Summary, 300))
	}
	t.Logf("diff scope=%s sha=%s truncated=%v bytes=%d", res.Scope, res.BaseSHA, res.Truncated, len(res.Summary))

	fs, ok := sess.(provider.ForkSession)
	if !ok {
		t.Fatal("session does not implement ForkSession")
	}
	forked, err := fs.Fork(ctx, provider.ForkOptions{})
	if err != nil {
		t.Fatalf("fork after goal materialization: %v", err)
	}
	if forked.AgentSessionID == "" {
		t.Fatal("fork returned empty child id")
	}
	if forked.ForkedFromID != "" && forked.ForkedFromID != sess.AgentSessionID() {
		t.Errorf("forkedFromId=%s want %s", forked.ForkedFromID, sess.AgentSessionID())
	}
	t.Logf("fork child=%s from=%s", forked.AgentSessionID, forked.ForkedFromID)
	if _, err := fs.Fork(ctx, provider.ForkOptions{LastTurnID: "not-a-turn-id-0080"}); err == nil {
		t.Fatal("fork with unknown lastTurnId should fail")
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
//
// gitDiffToRemote is a live v1 compatibility RPC; generate-json-schema emits
// the v2 diff notification name instead.
func TestLive0080SchemaSurface(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not found on PATH")
	}
	dir := t.TempDir()
	normal := filepath.Join(dir, "normal")
	experimental := filepath.Join(dir, "experimental")
	if err := os.MkdirAll(normal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(experimental, 0o755); err != nil {
		t.Fatal(err)
	}

	gen := func(out string, experimental bool) {
		t.Helper()
		args := []string{"app-server", "generate-json-schema", "--out", out}
		if experimental {
			args = append(args, "--experimental")
		}
		cmd := exec.Command("codex", args...)
		b, err := cmd.CombinedOutput()
		t.Logf("codex %s\n%s", strings.Join(args, " "), b)
		if err != nil {
			t.Fatalf("schema generate: %v", err)
		}
	}
	gen(normal, false)
	gen(experimental, true)

	normalBlob := readTree(t, normal)
	expBlob := readTree(t, experimental)
	for _, want := range []string{"review/start", "thread/fork", "thread/goal", "turn/diff/updated", "serviceTier", "personality"} {
		if !strings.Contains(normalBlob, want) {
			t.Errorf("normal schema missing %q", want)
		}
	}
	if !strings.Contains(expBlob, "collaborationMode") {
		t.Error("experimental schema missing collaborationMode")
	}
	if !strings.Contains(expBlob, "thread/settings/update") && !strings.Contains(expBlob, "ThreadSettingsUpdate") {
		t.Error("experimental schema missing thread/settings/update")
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
