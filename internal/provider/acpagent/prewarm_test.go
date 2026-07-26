package acpagent

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestClaimWarmRequiresMatchingProcDir(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	p := &Provider{log: slog.Default()}
	warm := &session{
		procDir: home,
		done:    make(chan struct{}),
		events:  make(chan event.Event, 1),
	}
	p.warm = warm

	if got := p.claimWarm(project); got != nil {
		t.Fatal("claimWarm must not hand out a spare whose process cwd differs from the session cwd")
	}
	if p.warm != warm {
		t.Fatal("mismatched claim must leave the spare in place for a later home/default session")
	}

	got := p.claimWarm(home)
	if got != warm {
		t.Fatal("claimWarm should return the spare when process cwd matches")
	}
	if p.warm != nil {
		t.Fatal("claimed spare must be removed from the pool")
	}
}

func TestClaimWarmRejectsDeadSpare(t *testing.T) {
	dir := t.TempDir()
	p := &Provider{log: slog.Default()}
	warm := &session{
		procDir:    dir,
		procExited: true,
		done:       make(chan struct{}),
		events:     make(chan event.Event, 1),
	}
	p.warm = warm

	if got := p.claimWarm(dir); got != nil {
		t.Fatal("dead spare must not be claimed")
	}
}

func TestResolveSessionCWDEmptyUsesHome(t *testing.T) {
	p := &Provider{log: slog.Default()}
	got, err := p.resolveSessionCWD("")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolveSessionCWD(\"\") = %q, want home %q", got, want)
	}
}

func TestResolveSessionCWDDefaultAndExplicit(t *testing.T) {
	def := t.TempDir()
	explicit := t.TempDir()
	p := &Provider{
		cfg: Config{DefaultCWD: def},
		log: slog.Default(),
	}

	got, err := p.resolveSessionCWD("")
	if err != nil {
		t.Fatal(err)
	}
	wantDef, err := filepath.Abs(def)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantDef {
		t.Fatalf("empty opts with DefaultCWD = %q, want %q", got, wantDef)
	}

	got, err = p.resolveSessionCWD(explicit)
	if err != nil {
		t.Fatal(err)
	}
	wantExplicit, err := filepath.Abs(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantExplicit {
		t.Fatalf("explicit CWD = %q, want %q", got, wantExplicit)
	}
}

func TestResolveSessionCWDRejectsMissing(t *testing.T) {
	p := &Provider{log: slog.Default()}
	_, err := p.resolveSessionCWD(filepath.Join(t.TempDir(), "no-such-dir"))
	if err == nil {
		t.Fatal("expected error for missing cwd")
	}
}
