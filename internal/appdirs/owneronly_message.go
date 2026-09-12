package appdirs

// NotOwnerOnlyDetail explains, for an operator, why [FileIsOwnerOnly] reported
// path as not private and how to fix it. It returns a clause meant to follow
// "is": "readable by <who>; run: <command>".
//
// It exists because every caller used to hand-write "chmod 0600", which is
// correct on Unix and meaningless on Windows, where mode bits carry no access
// control and the question FileIsOwnerOnly actually asks is about the owner
// SID and the DACL (MADR 0116 D22, MADR 0155 D4/F6). One helper, shared by
// mcremote and mcrelay, means the two daemons cannot drift apart again.
//
//   - Unix: "readable by group/other; run: chmod 0600 <path>".
//   - Windows: names each principal that can read the file, then an icacls
//     command that removes their access and leaves the caller as the only
//     trustee. The command is executed by TestNotOwnerOnlyDetailRemedyWorks
//     before it is ever printed to an operator.
//
// PLAN 0155 C1: the detail names the file and, on Windows, the principals that
// can read it. Never the file's contents.
//
// Callers add anything product-specific (mcremote's setup-service alternative)
// and the rotate-the-credential sentence (D9); this helper knows neither.
func NotOwnerOnlyDetail(path string) string {
	readers, remedy := notOwnerOnly(path)
	return "readable by " + readers + "; run: " + remedy
}
