package tools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
	"github.com/patchy-mcp/patchy/internal/update"
)

// RegisterUpdate registers the pd.ecosystem.update tool.
// Requires the update controller to be initialized separately.
func RegisterUpdate(s *server.MCPServer, uc *update.Controller) {
	tool := mcp.NewTool("pd.ecosystem.update",
		mcp.WithDescription("Update all PD tools and nuclei templates to their latest versions. Blocks tool execution during update."),
		mcp.WithArray("tools",
			mcp.Description("Specific tools to update. Empty = update all."),
			mcp.WithStringItems(),
		),
		mcp.WithBoolean("include_templates",
			mcp.Description("Also update nuclei templates. Default true."),
		),
		mcp.WithBoolean("include_pdtm",
			mcp.Description("Also self-update pdtm. Default true."),
		),
		mcp.WithBoolean("dry_run",
			mcp.Description("Show what would be updated without making changes."),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)

		cfg := update.UpdateConfig{
			IncludeTemplates: true,
			IncludePdtm:      true,
		}

		if tools := getStringArray(params, "tools"); len(tools) > 0 {
			cfg.Tools = tools
		}

		if it, ok := getBool(params, "include_templates"); ok {
			cfg.IncludeTemplates = it
		}
		if ip, ok := getBool(params, "include_pdtm"); ok {
			cfg.IncludePdtm = ip
		}
		if dr, ok := getBool(params, "dry_run"); ok {
			cfg.DryRun = dr
		}

		result, err := uc.Execute(ctx, cfg)
		if err != nil {
			if result != nil {
				data, _ := json.MarshalIndent(result, "", "  ")
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{mcp.NewTextContent(string(data))},
				}, nil
			}
			return mcpbridge.NewSimpleError("UPDATE_FAILED", err.Error()), nil
		}

		data, _ := json.MarshalIndent(result, "", "  ")
		return mcpbridge.NewTextResult(string(data)), nil
	})
}
