//go:build !windows

package service

import "errors"

// errNoTaskPrincipal is returned off Windows, where there is no Task Scheduler
// principal to resolve. Tests of the Windows branch inject taskPrincipalSID and
// taskAccountForSID instead.
var errNoTaskPrincipal = errors.New("the task principal is only resolvable on Windows")

func currentTaskPrincipalSID() (string, error) { return "", errNoTaskPrincipal }

func lookupTaskAccount(string) (string, error) { return "", errNoTaskPrincipal }
