package acpagent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestForkRequestOmitsOptionalForkOptions(t *testing.T) {
	got := forkRequest("sid-1", "/tmp/cwd")
	want := map[string]any{
		"sourceSessionId": "sid-1",
		"sourceCwd":       "/tmp/cwd",
		"newCwd":          "/tmp/cwd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, leak := range []string{"lastTurnId", "LastTurnID", "deferGoalContinuation", "sessionId", "cwd"} {
		if _, ok := got[leak]; ok {
			t.Errorf("payload leaked %s", leak)
		}
	}
}

func TestForkRejectsDeferGoalContinuation(t *testing.T) {
	s := &session{agentID: "s", cwd: "/tmp"}
	_, err := s.Fork(context.Background(), provider.ForkOptions{DeferGoalContinuation: true})
	if err == nil || !strings.Contains(err.Error(), "defer goal continuation") {
		t.Fatalf("err=%v", err)
	}
}

func TestForkClosedSession(t *testing.T) {
	s := &session{agentID: "s", cwd: "/tmp", closed: true}
	_, err := s.Fork(context.Background(), provider.ForkOptions{})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("err=%v", err)
	}
}

func TestForkMissingIDs(t *testing.T) {
	s := &session{cwd: "/tmp"}
	if _, err := s.Fork(context.Background(), provider.ForkOptions{}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("empty agentID err=%v", err)
	}
	s = &session{agentID: "s"}
	if _, err := s.Fork(context.Background(), provider.ForkOptions{}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("empty cwd err=%v", err)
	}
}
