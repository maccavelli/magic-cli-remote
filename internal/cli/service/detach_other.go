//go:build !windows

package service

// DetachConsole does nothing off Windows: only the Windows task gives the
// daemon a console (MADR 0159 D7).
func DetachConsole() {}
