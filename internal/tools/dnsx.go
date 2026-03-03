package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
)

func registerDnsx(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.dnsx.resolve",
		mcp.WithDescription("Resolve DNS records for hosts. Supports A, AAAA, CNAME, MX, NS, TXT, SOA, PTR, SRV, CAA."),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithArray("hosts",
			mcp.Description("List of hosts/domains to resolve."),
			mcp.WithStringItems(),
			mcp.Required(),
		),
		mcp.WithArray("record_types",
			mcp.Description("DNS record types to query (a, aaaa, cname, mx, ns, txt, soa, ptr, srv, caa). Default: a."),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("rate_limit",
			mcp.Description("Requests per second rate limit."),
		),
		mcp.WithNumber("retries",
			mcp.Description("Number of retries for failed queries. Default 2."),
		),
		mcp.WithBoolean("trace",
			mcp.Description("Enable DNS trace mode."),
		),
		mcp.WithBoolean("resp",
			mcp.Description("Display DNS response in output."),
		),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		hosts := getStringArray(params, "hosts")
		if len(hosts) == 0 {
			return mcpbridge.NewSimpleError("INVALID_INPUT", "hosts is required"), nil
		}

		var args []string
		args = append(args, "-l", "-") // read from stdin

		if rtypes := getStringArray(params, "record_types"); len(rtypes) > 0 {
			for _, rt := range rtypes {
				args = append(args, "-"+strings.ToLower(rt))
			}
		} else {
			args = append(args, "-a")
		}

		if rl, ok := getNumber(params, "rate_limit"); ok && rl > 0 {
			addFlagInt(&args, "-rl", int(rl))
		}

		if retries, ok := getNumber(params, "retries"); ok && retries > 0 {
			addFlagInt(&args, "-retry", int(retries))
		}

		if trace, ok := getBool(params, "trace"); ok && trace {
			addFlagBool(&args, "-trace", true)
		}

		if resp, ok := getBool(params, "resp"); ok && resp {
			addFlagBool(&args, "-resp", true)
		}

		outputFile := "dnsx_output.jsonl"

		return executeTool(ctx, deps, "pd.dnsx.resolve", "dnsx",
			hosts, args, []string{"-json"}, "DnsxRecord", outputFile)
	})
}
