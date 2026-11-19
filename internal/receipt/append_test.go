package receipt

import (
	"errors"
	"strings"
	"testing"
)

// fakeAppendFile fails the step named in failAt and records what was called.
type fakeAppendFile struct {
	failAt string // "write", "sync" or "close"
	calls  []string
}

func (f *fakeAppendFile) step(name string) error {
	f.calls = append(f.calls, name)
	if f.failAt == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeAppendFile) WriteString(string) (int, error) { return 0, f.step("write") }
func (f *fakeAppendFile) Sync() error                     { return f.step("sync") }
func (f *fakeAppendFile) Close() error                    { return f.step("close") }

func TestAppendLine(t *testing.T) {
	for _, tc := range []struct {
		name         string
		failAt       string
		wantDurable  bool
		wantErr      string // "" means no error
		wantSentinel bool   // errors.Is(err, ErrCloseAfterSync)
		wantCalls    string
	}{
		{"all steps succeed", "", true, "", false, "write,sync,close"},
		{"a failed write is reported, and the file is still closed", "write", false, "write p: write failed", false, "write,close"},
		{"a failed sync is reported, and the file is still closed", "sync", false, "sync p: sync failed", false, "write,sync,close"},
		// The case this function exists for: the file was written, so its close error is not dropped.
		{"a failed close after a good sync fails the call but the line is durable", "close", true, "close p: close failed: receipt written and synced", true, "write,sync,close"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAppendFile{failAt: tc.failAt}
			durable, err := appendLine(f, "p", "line\n")
			if durable != tc.wantDurable {
				t.Errorf("durable = %v, want %v", durable, tc.wantDurable)
			}
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("err = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr):
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
			if got := errors.Is(err, ErrCloseAfterSync); got != tc.wantSentinel {
				t.Errorf("errors.Is(err, ErrCloseAfterSync) = %v, want %v", got, tc.wantSentinel)
			}
			if got := strings.Join(f.calls, ","); got != tc.wantCalls {
				t.Errorf("calls = %s, want %s", got, tc.wantCalls)
			}
		})
	}
}
