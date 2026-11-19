package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func capturedLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestDrainAdminLogsAFailureTheAdminServerReportsAtShutdown(t *testing.T) {
	log, buf := capturedLog()
	ch := make(chan error, 1)
	ch <- errors.New("accept failed")
	drainAdmin(log, ch, time.Second)
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "accept failed") {
		t.Fatalf("log = %q, want a warning carrying the admin error", out)
	}
}

func TestDrainAdminIsSilentForACleanStop(t *testing.T) {
	log, buf := capturedLog()
	ch := make(chan error, 1)
	ch <- nil
	drainAdmin(log, ch, time.Second)
	if buf.Len() != 0 {
		t.Fatalf("log = %q, want nothing for a clean stop", buf.String())
	}
}

func TestDrainAdminGivesUpAfterTheWait(t *testing.T) {
	log, buf := capturedLog()
	start := time.Now()
	drainAdmin(log, make(chan error), 20*time.Millisecond)
	if time.Since(start) > 2*time.Second || buf.Len() != 0 {
		t.Fatalf("drainAdmin waited %s and logged %q; want a quiet return after the wait", time.Since(start), buf.String())
	}
}
