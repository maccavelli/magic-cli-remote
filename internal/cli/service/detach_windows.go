//go:build windows

package service

import "golang.org/x/sys/windows"

// procFreeConsole is kernel32's FreeConsole. x/sys/windows has no wrapper.
var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

// DetachConsole detaches the process from its console, so the console host
// Windows gave the task-launched daemon goes away (MADR 0159 D7, probe 7).
// It is best effort: a process without a console gets an error, which is
// ignored. After it, writes to stdout and stderr go nowhere.
func DetachConsole() {
	_, _, _ = procFreeConsole.Call()
}
