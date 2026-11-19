package relay

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestLogServeEnd(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantLog string // "" means nothing is logged
	}{
		{"nil", nil, ""},
		{"the server closed, which is what shutdown looks like", http.ErrServerClosed, ""},
		{"the server closed, wrapped", errors.Join(errors.New("ctx"), http.ErrServerClosed), ""},
		{"a failure is a warning", errors.New("http serve panic: boom"), "http serve panic: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &Server{log: slog.New(slog.NewTextHandler(&buf, nil))}
			s.logServeEnd(tc.err)
			got := buf.String()
			if tc.wantLog == "" && got != "" {
				t.Fatalf("logged %q, want nothing", got)
			}
			if tc.wantLog != "" && (!strings.Contains(got, tc.wantLog) || !strings.Contains(got, "level=WARN")) {
				t.Fatalf("log = %q, want a warning containing %q", got, tc.wantLog)
			}
		})
	}
}
