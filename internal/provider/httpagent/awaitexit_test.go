package httpagent

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestAwaitExitLogsHowTheKilledEngineEnded(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "engine-bin"}, log)

	ch := make(chan error, 1)
	ch <- errors.New("signal: killed")
	p.awaitExit(ch)
	if out := buf.String(); !strings.Contains(out, "signal: killed") || !strings.Contains(out, "engine-bin") {
		t.Fatalf("log = %q, want the exit status and the engine binary", out)
	}

	buf.Reset()
	ch <- nil
	p.awaitExit(ch)
	if buf.Len() != 0 {
		t.Fatalf("log = %q, want nothing for a clean exit", buf.String())
	}
}
