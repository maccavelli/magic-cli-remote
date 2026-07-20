package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{" debug ", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"invalid", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tc := range tests {
		got := parseLevel(tc.input)
		if got != tc.want {
			t.Errorf("parseLevel(%q) = %v; want %v", tc.input, got, tc.want)
		}
	}
}

func TestSetupJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup(Options{
		Level:  "debug",
		Format: "json",
		Out:    &buf,
	})

	if logger == nil {
		t.Fatal("Setup returned nil logger")
	}

	logger.Info("test message", slog.String("key", "val"))

	out := buf.String()
	if !strings.Contains(out, `"msg":"test message"`) {
		t.Errorf("JSON output did not contain message: %q", out)
	}
	if !strings.Contains(out, `"key":"val"`) {
		t.Errorf("JSON output did not contain key/value: %q", out)
	}
}

func TestSetupText(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup(Options{
		Level:  "warn",
		Format: "text",
		Out:    &buf,
	})

	if logger == nil {
		t.Fatal("Setup returned nil logger")
	}

	logger.Debug("should not see this")
	logger.Warn("test message", slog.String("key", "val"))

	out := buf.String()
	if strings.Contains(out, "should not see this") {
		t.Errorf("Output contained filtered debug message: %q", out)
	}
	if !strings.Contains(out, "msg=\"test message\"") && !strings.Contains(out, "msg=test message") {
		t.Errorf("Text output did not contain message: %q", out)
	}
	if !strings.Contains(out, "key=val") {
		t.Errorf("Text output did not contain key/value: %q", out)
	}
}

func TestSetupDefaultOut(t *testing.T) {
	logger := Setup(Options{
		Level:  "info",
		Format: "text",
	})
	if logger == nil {
		t.Fatal("Setup returned nil logger with default output")
	}
}
