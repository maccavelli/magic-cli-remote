//go:build !linux

package service

// procParent is nil off Linux, so Inside reports false there: no script
// backend runs off Linux. runit, s6 and OpenRC are resolved only on Linux
// (backend.go).
var procParent func(pid int) (int, error)
