//go:build windows

package service

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// registryPath is the User PATH: the Path value of HKCU\Environment.
type registryPath struct{}

func (registryPath) get() (string, uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", registry.EXPAND_SZ, nil
	}
	return v, typ, err
}

func (registryPath) set(value string, typ uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if typ == registry.EXPAND_SZ {
		return k.SetExpandStringValue("Path", value)
	}
	return k.SetStringValue("Path", value)
}

func (registryPath) broadcast() { broadcastEnvironmentChange() }

// userPath is the store the folder move writes. It is a variable for tests.
var userPath pathStore = registryPath{}
