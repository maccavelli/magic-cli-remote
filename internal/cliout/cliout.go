// Package cliout writes a command's human-readable output.
//
// A command prints status lines to the writer it was given (a cobra command's
// OutOrStdout or ErrOrStderr, or a buffer in a test). A failed write there,
// a closed pipe or a closed terminal, has nowhere better to be reported than the
// stream that just failed, and it does not change what the command did: the
// operation the lines describe has already happened, and its own error is what
// the command returns. So the error is dropped, here, in one place, instead of
// at each of the hundreds of lines that print (MADR 0131, P5, Q3 A).
//
// Use it for that and only for that: status lines. Output that is the command's
// result, not a status line, returns its write error to the command, so a
// script that sent it to a file or a pipe finds out when it did not arrive:
//
//   - a generated file body or a JSON document (setup-service --print-only and
//     --refresh --print-only, in mcremote and in mcrelay; the --json outputs,
//     which return the encoder's error);
//   - a statement of record (receipts show: the verdict and the statement);
//   - a value shown once (pair create's token and its pairing URI, pair code's
//     code and its URI);
//   - a result a script acts on (auth recovery status's lines, receipts verify's
//     OK or BROKEN line, where a failed write is added to a broken chain's error
//     and never put in its place).
//
// Those sites call fmt.Fprint* and check the error (MADR 0131, P5, R2 and R3).
// auth recovery choose's confirmation stays a status line: its outcome is in its
// exit status.
//
// A text/tabwriter is the one other writer these functions are given: it
// buffers, and reports a failed write from its Flush, which its caller checks.
package cliout

import (
	"fmt"
	"io"
)

// Fprintf formats to w, as fmt.Fprintf does, and drops the write error.
func Fprintf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

// Fprintln writes its operands to w, as fmt.Fprintln does, and drops the write error.
func Fprintln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

// Fprint writes its operands to w, as fmt.Fprint does, and drops the write error.
func Fprint(w io.Writer, a ...any) {
	_, _ = fmt.Fprint(w, a...)
}
