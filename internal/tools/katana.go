package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerKatana(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.katana.crawl",
		mcp.WithDescription("Web crawler that discovers endpoints, forms, JavaScript files, and more."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithArray("targets",
			mcp.Description("URLs to crawl."),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithNumber("depth",
			mcp.Description("Crawl depth. Default 3."),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Requests per second rate limit."),
		),
		mcp.WithBoolean("js_crawl",
			mcp.Description("Enable JavaScript file crawling."),
		),
		mcp.WithBoolean("headless",
			mcp.Description("Use headless browser for crawling (if allowed by policy)."),
		),
		mcp.WithString("scope",
			mcp.Description("Crawl scope: dn (domain name), rdn (root domain), fqdn."),
		),
		mcp.WithArray("exclude",
			mcp.Description("URL patterns to exclude from crawling."),
			mcp.WithStringItems(),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		targets := getStringArray(params, "targets")
		if len(targets) == 0 {
			return mcpbridge.NewSimpleError("INVALID_INPUT", "targets is required"), nil
		}

		var args []string
		args = append(args, "-list", "-") // read from stdin

		if depth, ok := getNumber(params, "depth"); ok && depth > 0 {
			addFlagInt(&args, "-d", int(depth))
		}

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rl", int(rl))
		}

		if jsCrawl, ok := getBool(params, "js_crawl"); ok && jsCrawl {
			addFlagBool(&args, "-jc", true)
		}

		if headless, ok := getBool(params, "headless"); ok && headless {
			addFlagBool(&args, "-headless", true)
		}

		if scope := getString(params, "scope"); scope != "" {
			addFlag(&args, "-cs", scope)
		}

		if exclude := getStringArray(params, "exclude"); len(exclude) > 0 {
			addFlag(&args, "-ef", strings.Join(exclude, ","))
		}

		outputFile := "katana_output.jsonl"

		return executeTool(ctx, deps, "pd.katana.crawl", "katana",
			targets, args, []string{"-jsonl"}, "KatanaRecord", outputFile)
	})
}
