//go:build windows

package service

import (
	"fmt"

	"golang.org/x/sys/windows"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// currentTaskPrincipalSID returns the calling process token's user SID — the
// form Task Scheduler stores a principal in (MADR 0159 probe 5). Unlike
// USERNAME/USERDOMAIN it cannot be overridden from the environment (F11).
func currentTaskPrincipalSID() (string, error) {
	sid, err := appdirs.CurrentUserSID()
	if err != nil {
		return "", fmt.Errorf("read the process token's user SID: %w", err)
	}
	return sid.String(), nil
}

// lookupTaskAccount resolves a SID to DOMAIN\name for the logon trigger and
// the author field.
func lookupTaskAccount(sid string) (string, error) {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return "", fmt.Errorf("parse SID %q: %w", sid, err)
	}
	account, domain, _, err := s.LookupAccount("")
	if err != nil {
		return "", fmt.Errorf("look up the account for %s: %w", sid, err)
	}
	if domain == "" {
		return account, nil
	}
	return domain + `\` + account, nil
}
