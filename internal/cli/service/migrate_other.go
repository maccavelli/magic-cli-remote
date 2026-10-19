//go:build !windows

package service

import "errors"

// noPath is the User PATH off Windows, where the folder move never runs
// outside tests.
type noPath struct{}

func (noPath) get() (string, uint32, error) {
	return "", 0, errors.New("the User PATH is a Windows registry value")
}
func (noPath) set(string, uint32) error {
	return errors.New("the User PATH is a Windows registry value")
}
func (noPath) broadcast() {}

// userPath is the store the folder move writes. It is a variable for tests.
var userPath pathStore = noPath{}
