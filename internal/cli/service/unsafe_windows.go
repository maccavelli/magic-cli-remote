//go:build windows

package service

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// broadcastEnvironmentChange tells running programs the User environment
// changed, as install.ps1's Send-EnvironmentChange does
// (scripts/install.ps1:245-259): HWND_BROADCAST, WM_SETTINGCHANGE,
// "Environment", SMTO_ABORTIFHUNG, 5 s. Best effort: a new terminal sees the
// change regardless. The package's one use of unsafe is kept to this file.
func broadcastEnvironmentChange() {
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x1A, 0x2
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000,
		uintptr(unsafe.Pointer(&result)))
}
