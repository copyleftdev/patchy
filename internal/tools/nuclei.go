package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerNuclei(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.nuclei.scan",
		mcp.WithDescription("Run nuclei vulnerability scanner against targets using template-based detection."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithArray("targets",
			mcp.Description("URLs or hosts to scan."),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithArray("tags",
			mcp.Description("Template tags to filter by (e.g., cve, xss, sqli)."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("severity",
			mcp.Description("Severity filter: info, low, medium, high, critical."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("templates",
			mcp.Description("Specific template IDs to run."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("exclude_tags",
			mcp.Description("Template tags to exclude."),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Requests per second rate limit."),
		),
		mcp.WithNumber("concurrency",
			mcp.Description("Number of concurrent template executions."),
		),
		mcp.WithBoolean("new_templates",
			mcp.Description("Only run newly added templates."),
		),
		mcp.WithBoolean("automatic_scan",
			mcp.Description("Enable automatic web scan (maps technologies to templates)."),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		targets := getStringArray(params, "targets")
		if len(targets) == 0 {
			return mcpbridge.NewSimpleError("INVALID_INPUT", "targets is required"), nil
		}

		var args []string
		// nuclei reads targets from stdin automatically (no -l flag needed)

		if tags := getStringArray(params, "tags"); len(tags) > 0 {
			addFlag(&args, "-tags", strings.Join(tags, ","))
		}

		if severity := getStringArray(params, "severity"); len(severity) > 0 {
			addFlag(&args, "-severity", strings.Join(severity, ","))
		}

		if templates := getStringArray(params, "templates"); len(templates) > 0 {
			addFlag(&args, "-t", strings.Join(templates, ","))
		}

		if excludeTags := getStringArray(params, "exclude_tags"); len(excludeTags) > 0 {
			addFlag(&args, "-etags", strings.Join(excludeTags, ","))
		}

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rl", int(rl))
		}

		if conc, ok := getNumber(params, "concurrency"); ok && conc > 0 {
			addFlagInt(&args, "-c", int(conc))
		}

		if newT, ok := getBool(params, "new_templates"); ok && newT {
			addFlagBool(&args, "-nt", true)
		}

		if autoScan, ok := getBool(params, "automatic_scan"); ok && autoScan {
			addFlagBool(&args, "-as", true)
		}

		outputFile := "nuclei_output.jsonl"

		return executeTool(ctx, deps, "pd.nuclei.scan", "nuclei",
			targets, args, []string{"-jsonl"}, "NucleiRecord", outputFile)
	})
}
