//go:build windows

package procutil

import "unsafe"

// unsafePointer and unsafeSizeof keep the single unsafe usage in this package
// isolated to one small file, so the job-object plumbing above reads as
// ordinary Go.
func unsafePointer[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }

func unsafeSizeof[T any](v T) uintptr { return unsafe.Sizeof(v) }
