package httpagent

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// An engine that dies instantly (bad binary, immediate crash) must fail startup
// at once, not spin the full serverStartTimeout probing a corpse on
// connection-refused. `false` exits non-zero immediately, so the health poll
// never connects; the fix watches cmd.Wait and bails as soon as it exits.
func TestStartServerBailsWhenEngineExitsImmediately(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("no 'false' binary on PATH")
	}
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	start := time.Now()
	_, err := p.startServer(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when engine exits immediately")
	}
	// Must return promptly — well under serverStartTimeout, which the buggy code
	// would spin in full before failing.
	if elapsed > 5*time.Second {
		t.Fatalf("startServer took %s; expected prompt failure well under serverStartTimeout (%s)",
			elapsed, serverStartTimeout)
	}
	if !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("error=%q, want it to mention 'exited during startup'", err)
	}
}
