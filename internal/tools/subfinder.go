package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerSubfinder(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.subfinder.enumerate",
		mcp.WithDescription("Discover subdomains for a given domain using multiple passive sources."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithString("domain",
			mcp.Description("Target domain to enumerate subdomains for."),
			mcp.Required(),
		),
		mcp.WithArray("sources",
			mcp.Description("Specific sources to use (e.g., crtsh, hackertarget). Empty = all."),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Requests per second rate limit."),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Timeout in minutes. Default 5."),
		),
		mcp.WithBoolean("recursive",
			mcp.Description("Enable recursive subdomain enumeration."),
		),
		mcp.WithBoolean("all",
			mcp.Description("Use all sources including slow ones."),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		domain := getString(params, "domain")
		if domain == "" {
			return mcpbridge.NewSimpleError("INVALID_INPUT", "domain is required"), nil
		}

		targets := []string{domain}
		var args []string

		args = append(args, "-d", domain)

		if sources := getStringArray(params, "sources"); len(sources) > 0 {
			for _, src := range sources {
				args = append(args, "-s", src)
			}
		}

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rl", int(rl))
		}

		if recursive, ok := getBool(params, "recursive"); ok && recursive {
			addFlagBool(&args, "-recursive", true)
		}

		if all, ok := getBool(params, "all"); ok && all {
			addFlagBool(&args, "-all", true)
		}

		outputFile := fmt.Sprintf("subfinder_%s.jsonl", domain)

		return executeTool(ctx, deps, "pd.subfinder.enumerate", "subfinder",
			targets, args, []string{"-json"}, "SubfinderRecord", outputFile)
	})
}
