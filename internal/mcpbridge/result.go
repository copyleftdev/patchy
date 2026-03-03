package mcpbridge

import (
	"encoding/json"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/patchy-mcp/patchy/pkg/schema"
)

// NewResult creates a successful MCP tool result containing a RunResult.
func NewResult(rr *schema.RunResult) *mcp.CallToolResult {
	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		slog.Warn("result_marshal_failed", "error", err)
		data = []byte(`{"error":"marshal failed"}`)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(data)),
		},
	}
}

// NewTextResult creates a successful MCP tool result from raw JSON text.
func NewTextResult(jsonText string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(jsonText),
		},
	}
}

// NewErrorResult creates an MCP error result.
func NewErrorResult(rr *schema.RunResult) *mcp.CallToolResult {
	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		slog.Warn("result_marshal_failed", "error", err)
		data = []byte(`{"error":"marshal failed"}`)
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			mcp.NewTextContent(string(data)),
		},
	}
}

// NewSimpleError creates an MCP error result from a code and message.
func NewSimpleError(code, message string) *mcp.CallToolResult {
	rr := &schema.RunResult{
		SchemaVersion: schema.RunResultSchemaVersion,
		Status:        "error",
		Error: &schema.ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
	return NewErrorResult(rr)
}
