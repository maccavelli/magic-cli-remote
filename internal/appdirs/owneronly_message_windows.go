//go:build windows

package appdirs

import (
	"strings"

	"golang.org/x/sys/windows"
)

// notOwnerOnly is the Windows half of NotOwnerOnlyDetail.
//
// It re-reads the file's owner and DACL and reports the same principals that
// made FileIsOwnerOnly false: an owner other than the caller, then every
// foreign ALLOW trustee from foreignTrustees (which shares noForeignTrustee's
// allow-list). Names come from SID.LookupAccount, falling back to the SID
// string, so a localised or deleted account still produces something an
// operator can act on.
//
// The remedy is one icacls command. /inheritance:r drops inherited ACEs,
// /remove:g drops each foreign trustee's explicit grant, and /grant:r gives
// the caller full control, which is needed because removing inheritance can
// otherwise remove the caller's own access. Trustees are passed as *SID, not
// names, because names are localised and can be ambiguous across domains.
//
// If the security information cannot be read, it says so plainly rather than
// guessing a principal. FileIsOwnerOnly has already failed for a reason this
// function cannot see, and inventing a name would send the operator after the
// wrong fix.
func notOwnerOnly(path string) (readers, remedy string) {
	abs, err := absClean(path, "path")
	if err != nil {
		return "another principal", "check the file's permissions in its Security properties"
	}
	self, err := currentUserSID()
	if err != nil {
		return "another principal", "check the file's permissions in its Security properties"
	}
	sd, err := windows.GetNamedSecurityInfo(abs, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "another principal", "check the file's permissions in its Security properties"
	}

	var names []string
	cmd := []string{`icacls "` + abs + `" /inheritance:r`}

	if owner, _, oerr := sd.Owner(); oerr == nil && !owner.Equals(self) {
		names = append(names, accountName(owner)+" (the file's owner)")
		// Removing access cannot change ownership; only a file the caller owns
		// can pass FileIsOwnerOnly. Say so first, since icacls alone will not
		// fix this case.
		cmd = []string{`copy the file to one you own, then icacls "<copy>" /inheritance:r`}
	}

	for _, t := range foreignTrustees(sd.String(), self) {
		sid, serr := windows.StringToSid(t)
		if serr != nil {
			names = append(names, "an unrecognised access entry")
			continue
		}
		names = append(names, accountName(sid))
		cmd = append(cmd, "/remove:g *"+sid.String())
	}
	cmd = append(cmd, "/grant:r *"+self.String()+":F")

	if len(names) == 0 {
		// FileIsOwnerOnly said no, but nothing here explains why. Do not claim
		// a principal that is not there.
		return "another principal", strings.Join(cmd, " ")
	}
	return strings.Join(names, ", "), strings.Join(cmd, " ")
}

// accountName renders a SID as DOMAIN\name, or as the SID string when it does
// not resolve.
func accountName(sid *windows.SID) string {
	account, domain, _, err := sid.LookupAccount("")
	if err != nil || account == "" {
		return sid.String()
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}
