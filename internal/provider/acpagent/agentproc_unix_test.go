//go:build unix

package acpagent

import (
	"context"
	"syscall"
	"testing"
)

// TestSpawnedAgentHasItsOwnProcessGroup is MADR 0207 D10 on Unix: the agent
// leads a process group of its own, so a signal to the group reaches the
// processes it starts. Windows puts the agent in a job instead.
func TestSpawnedAgentHasItsOwnProcessGroup(t *testing.T) {
	p, pidFile := testAgentProvider(t)
	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	pid := testAgentPID(t, pidFile)
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != pid {
		t.Fatalf("the agent (pid %d) is in process group %d; want a group of its own", pid, pgid)
	}
}
