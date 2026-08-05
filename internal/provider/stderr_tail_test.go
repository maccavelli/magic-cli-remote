package provider

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// MADR 0069 P3 (U7) — the stderr tail a provider embeds in a phone-visible
// start error is warn-logged at the same moment. Previously the identical
// lines were logged only at Debug, invisible at the default level.
func TestLogStderrTail(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	LogStderrTail(log, "goose", "boom line 1\nboom line 2")
	out := buf.String()
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "boom line 1") {
		t.Fatalf("tail not warn-logged: %s", out)
	}

	buf.Reset()
	LogStderrTail(log, "goose", "")
	if buf.Len() != 0 {
		t.Fatalf("empty tail must not log: %s", buf.String())
	}
	LogStderrTail(nil, "goose", "x") // must not panic
}
