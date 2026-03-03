package mcpbridge

import (
	"github.com/mark3labs/mcp-go/server"
)

// ServerConfig holds MCP server configuration.
type ServerConfig struct {
	Name      string
	Version   string
	Transport string // stdio | sse | streamable-http
	Listen    string // for non-stdio transports
}

// NewServer creates a configured MCP server instance.
func NewServer(cfg ServerConfig) *server.MCPServer {
	return server.NewMCPServer(cfg.Name, cfg.Version,
		server.WithToolCapabilities(true),
		server.WithTaskCapabilities(true, true, true),
	)
}
