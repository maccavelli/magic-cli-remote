package cli

import "github.com/maccavelli/go-tui-lib/v2/safetext"

// termLine makes text Magic did not write safe for one line of the operator's
// terminal: a device name, chosen by whoever held a pair code. Names stored
// before the store cleaned them are printed as stored, so every print of a name
// goes through here (MADR 0219 D3). A clean name is returned as it is.
func termLine(s string) string { return safetext.Clean(s, safetext.Line()) }
