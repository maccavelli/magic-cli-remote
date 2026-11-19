package session

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
)

func receiptLogManager() (*Manager, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return NewManager(provider.NewRegistry(), nil, log, nil), &buf
}

// A receipt that was appended and only failed to close is logged as that, at
// the level of the existing line; any other failure keeps the old text.
func TestReceiptAppendFailureLogsAppendedButCloseFailedOnlyForThatError(t *testing.T) {
	closeAfterSync := fmt.Errorf("close /d/dev.jsonl: %w: %w", errors.New("close failed"), receipt.ErrCloseAfterSync)
	writeFailed := errors.New("write /d/dev.jsonl: disk full")

	for _, tc := range []struct {
		name, failed, closed, want string
		err                        error
	}{
		{"a receipt, close after sync", msgReceiptAppendFailed, msgReceiptAppendedCloseFailed,
			`msg="receipt: appended, but closing its file failed"`, closeAfterSync},
		{"a receipt, any other failure", msgReceiptAppendFailed, msgReceiptAppendedCloseFailed,
			`msg="receipt: append failed"`, writeFailed},
		{"a marker, close after sync", msgMarkerAppendFailed, msgMarkerAppendedCloseFailed,
			`msg="receipt: appended unavailable marker, but closing its file failed"`, closeAfterSync},
		{"a marker, any other failure", msgMarkerAppendFailed, msgMarkerAppendedCloseFailed,
			`msg="receipt: append unavailable marker failed"`, writeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, buf := receiptLogManager()
			m.logReceiptAppendFailure(tc.failed, tc.closed, "dev-1", tc.err)
			out := buf.String()
			if !strings.Contains(out, "level=WARN") || !strings.Contains(out, tc.want) ||
				!strings.Contains(out, "device_id=dev-1") || strings.Count(out, "\n") != 1 {
				t.Fatalf("log = %q, want one WARN line with %s and the device id", out, tc.want)
			}
		})
	}
}

// The exact wording is part of the contract: operators search the log for it.
func TestReceiptAppendFailureMessagesAreExact(t *testing.T) {
	for got, want := range map[string]string{
		msgReceiptAppendFailed:        "receipt: append failed",
		msgReceiptAppendedCloseFailed: "receipt: appended, but closing its file failed",
		msgMarkerAppendFailed:         "receipt: append unavailable marker failed",
		msgMarkerAppendedCloseFailed:  "receipt: appended unavailable marker, but closing its file failed",
	} {
		if got != want {
			t.Errorf("message = %q, want %q", got, want)
		}
	}
}
