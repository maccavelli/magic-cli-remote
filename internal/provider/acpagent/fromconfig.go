package acpagent

import (
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// ConfigFrom builds a Config from the config file's shared ACP keys. Every ACP
// CLI agent's section converts through this one function, so they stay
// identical in how config maps to the adapter.
func ConfigFrom(c config.ACPProviderConfig) Config {
	mcp := make([]McpServer, 0, len(c.MCPServers))
	for _, m := range c.MCPServers {
		mcp = append(mcp, McpServer{
			Name:      m.Name,
			Transport: McpTransport(m.Transport),
			URL:       m.URL,
			Headers:   m.Headers,
		})
	}
	return Config{
		Bin:               c.Bin,
		Args:              c.Args,
		AlwaysApprove:     c.AlwaysApprove,
		DefaultCWD:        c.DefaultCWD,
		Model:             c.Model,
		PermissionTimeout: time.Duration(c.PermissionTimeoutSeconds) * time.Second,
		Prewarm:           c.Prewarm,
		TurnStallNotice:   time.Duration(c.TurnStallNoticeSeconds) * time.Second,
		FSRoots:           c.FSRoots,
		McpServers:        mcp,
		AuthMethodID:      c.AuthMethodID,
	}
}
