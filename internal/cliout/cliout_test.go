package cliout

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

// failingWriter reports a write error and counts the calls.
type failingWriter struct{ calls int }

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, errors.New("closed pipe")
}

func TestFunctionsWriteWhatFmtWrites(t *testing.T) {
	var got, want bytes.Buffer
	Fprintf(&got, "%s=%d\n", "n", 7)
	Fprintln(&got, "a", 1, "b")
	Fprint(&got, "x", 2, 3, "y")
	_, _ = fmt.Fprintf(&want, "%s=%d\n", "n", 7)
	_, _ = fmt.Fprintln(&want, "a", 1, "b")
	_, _ = fmt.Fprint(&want, "x", 2, 3, "y")
	if got.String() != want.String() {
		t.Fatalf("output = %q, want %q", got.String(), want.String())
	}
}

// The point of the package: a failing writer neither panics nor aborts the caller.
func TestAFailedWriteIsDroppedAndTheCallerContinues(t *testing.T) {
	w := &failingWriter{}
	Fprintf(w, "one %d\n", 1)
	Fprintln(w, "two")
	Fprint(w, "three")
	if w.calls != 3 {
		t.Fatalf("writer was called %d times, want 3 (each call must reach the writer once)", w.calls)
	}
}
