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
}
