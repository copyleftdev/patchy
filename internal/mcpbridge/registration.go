package mcpbridge

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ToolHandler is the handler function signature for MCP tool calls.
type ToolHandler func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)

// RegisterTool wraps SDK tool registration.
func RegisterTool(s *server.MCPServer, tool mcp.Tool, handler ToolHandler) {
	s.AddTool(tool, server.ToolHandlerFunc(handler))
}
