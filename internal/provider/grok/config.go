package grok

// Config configures the Grok Build ACP provider.
type Config struct {
	// Bin is the grok executable (default "grok").
	Bin string
	// Args are appended after the binary. Default: agent --no-leader stdio
	// Global agent flags (e.g. --always-approve) go before "stdio".
	Args []string
	// AlwaysApprove auto-selects the first "allow"-style permission option
	// without remoting to the client. Prefer remote permissions for phones.
	AlwaysApprove bool
	// DefaultCWD is used when StartOptions.CWD is empty.
	DefaultCWD string
	// Model is passed as -m when non-empty.
	Model string
}
