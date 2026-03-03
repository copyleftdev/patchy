package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerHttpx(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.httpx.probe",
		mcp.WithDescription("Probe HTTP services for live hosts, status codes, titles, technologies, and more."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithArray("targets",
			mcp.Description("URLs or hosts to probe."),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Requests per second rate limit."),
		),
		mcp.WithNumber("threads",
			mcp.Description("Number of concurrent threads."),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Timeout in seconds per request."),
		),
		mcp.WithBoolean("title",
			mcp.Description("Extract page title."),
		),
		mcp.WithBoolean("status_code",
			mcp.Description("Include HTTP status code."),
		),
		mcp.WithBoolean("content_length",
			mcp.Description("Include content length."),
		),
		mcp.WithBoolean("tech_detect",
			mcp.Description("Enable technology detection (Wappalyzer)."),
		),
		mcp.WithBoolean("follow_redirects",
			mcp.Description("Follow HTTP redirects."),
		),
		mcp.WithString("method",
			mcp.Description("HTTP method to use (GET, HEAD, POST, etc.)."),
		),
		mcp.WithArray("match_codes",
			mcp.Description("Only include responses with these status codes."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("filter_codes",
			mcp.Description("Exclude responses with these status codes."),
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
		// httpx reads targets from stdin directly (no -l flag needed)

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rl", int(rl))
		}
		if threads, ok := getNumber(params, "threads"); ok && threads > 0 {
			addFlagInt(&args, "-threads", int(threads))
		}
		if timeout, ok := getNumber(params, "timeout"); ok && timeout > 0 {
			addFlagInt(&args, "-timeout", int(timeout))
		}
		if title, ok := getBool(params, "title"); ok && title {
			addFlagBool(&args, "-title", true)
		}
		if sc, ok := getBool(params, "status_code"); ok && sc {
			addFlagBool(&args, "-status-code", true)
		}
		if cl, ok := getBool(params, "content_length"); ok && cl {
			addFlagBool(&args, "-content-length", true)
		}
		if td, ok := getBool(params, "tech_detect"); ok && td {
			addFlagBool(&args, "-tech-detect", true)
		}
		if fr, ok := getBool(params, "follow_redirects"); ok && fr {
			addFlagBool(&args, "-follow-redirects", true)
		}
		if method := getString(params, "method"); method != "" {
			addFlag(&args, "-x", method)
		}
		if mc := getStringArray(params, "match_codes"); len(mc) > 0 {
			for _, code := range mc {
				addFlag(&args, "-mc", code)
			}
		}
		if fc := getStringArray(params, "filter_codes"); len(fc) > 0 {
			for _, code := range fc {
				addFlag(&args, "-fc", code)
			}
		}

		outputFile := "httpx_output.jsonl"

		return executeTool(ctx, deps, "pd.httpx.probe", "httpx",
			targets, args, []string{"-json"}, "HttpxRecord", outputFile)
	})
}
