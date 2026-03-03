package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerNaabu(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.naabu.scan",
		mcp.WithDescription("Fast port scanning of hosts/IPs. Discovers open TCP/UDP ports."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithArray("hosts",
			mcp.Description("Hosts or IPs to scan."),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithString("ports",
			mcp.Description("Port specification (e.g., '80,443', '1-1024', 'top-100')."),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Packets per second rate limit."),
		),
		mcp.WithNumber("retries",
			mcp.Description("Number of retries per port. Default 3."),
		),
		mcp.WithBoolean("top_ports",
			mcp.Description("Scan top 100 ports instead of full range."),
		),
		mcp.WithString("scan_type",
			mcp.Description("Scan type: connect (default) or syn (requires allow_syn_scan policy)."),
		),
		mcp.WithArray("exclude_ports",
			mcp.Description("Ports to exclude from scan."),
			mcp.WithStringItems(),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		hosts := getStringArray(params, "hosts")
		if len(hosts) == 0 {
			return mcpbridge.NewSimpleError("INVALID_INPUT", "hosts is required"), nil
		}

		var args []string
		args = append(args, "-host", strings.Join(hosts, ","))

		if ports := getString(params, "ports"); ports != "" {
			addFlag(&args, "-p", ports)
		}

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rate", int(rl))
		}

		if retries, ok := getNumber(params, "retries"); ok && retries > 0 {
			addFlagInt(&args, "-retries", int(retries))
		}

		if topPorts, ok := getBool(params, "top_ports"); ok && topPorts {
			addFlag(&args, "-top-ports", "100")
		}

		if scanType := getString(params, "scan_type"); scanType != "" {
			addFlag(&args, "-s", scanType)
		}

		if ep := getStringArray(params, "exclude_ports"); len(ep) > 0 {
			addFlag(&args, "-exclude-ports", strings.Join(ep, ","))
		}

		outputFile := "naabu_output.jsonl"

		return executeTool(ctx, deps, "pd.naabu.scan", "naabu",
			hosts, args, []string{"-json"}, "NaabuRecord", outputFile)
	})
}
