package acphttp

import "time"

// McpServer describes one MCP server to forward to the engine.
type McpServer struct {
	Name      string
	Transport string
	URL       string
	Headers   map[string]string
}

// Config holds user-supplied options for an ACP-over-HTTP provider.
type Config struct {
	Bin               string
	AlwaysApprove     bool
	DefaultCWD        string
	Model             string
	PermissionTimeout time.Duration
	Prewarm           bool
	TurnStallNotice   time.Duration
	AuthMethodID      string
	McpServers        []McpServer
	// StreamCoalesce is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024). The
	// first chunk of a run and the tail before any control event are never
	// delayed, so time-to-first-token and end-of-turn latency are unchanged;
	// only mid-stream granularity is capped. nil means
	// defaultStreamCoalesce; an explicit 0 disables coalescing entirely
	// (exact pre-0024 behaviour, one event per token).
	StreamCoalesce *time.Duration
}

// StreamCoalesceWindow reports the streaming-text coalescing window, defaulting
// to defaultStreamCoalesce when unset. Zero means coalescing is off (MADR 0024).
func (c Config) StreamCoalesceWindow() time.Duration {
	if c.StreamCoalesce == nil {
		return defaultStreamCoalesce
	}
	return *c.StreamCoalesce
}
