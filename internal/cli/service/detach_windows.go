//go:build windows

package service

import (
	"golang.org/x/sys/windows"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// procFreeConsole is kernel32's FreeConsole. x/sys/windows has no wrapper.
var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

// DetachConsole detaches the process from its console, so the console host
// Windows gave the task-launched daemon goes away (MADR 0159 D7, probe 7).
// It is best effort: a process without a console gets an error, which is
// ignored. After it, writes to stdout and stderr go nowhere.
//
// Telling procutil is not optional bookkeeping. Every child started after this
// point must be given CREATE_NO_WINDOW, or it is handed a console of its own
// with a visible window (MADR 0159 F22), and the polite stop must know to borrow
// a child's console rather than use one we no longer have (0159 D22).
func DetachConsole() {
	_, _, _ = procFreeConsole.Call()
	procutil.NoteConsoleDetached()
}
