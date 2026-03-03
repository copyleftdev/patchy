# PATCHY

MCP server that wraps the [ProjectDiscovery](https://projectdiscovery.io/) security toolbox into typed, policy-enforced tools for LLM agents.

PATCHY exposes **subfinder**, **dnsx**, **httpx**, **naabu**, **katana**, and **nuclei** as structured [Model Context Protocol](https://modelcontextprotocol.io/) tools with scope enforcement, sandboxed execution, and normalized JSON output.

## Quick Start

```bash
# Build
make build

# Set your target scope and run
PATCHY_SCOPE=example.com ./bin/patchy

# Or with a config file
./bin/patchy --config patchy.yaml
```

### First Run — Nothing Installed

PATCHY can bootstrap itself. Connect to the server and call:

```
pd.ecosystem.setup
```

This installs pdtm, all PD tools, and nuclei templates automatically.

Or run the doctor to see what's missing:

```bash
PATCHY_SCOPE=example.com ./bin/patchy --doctor
```

### MCP Host Configuration

**Claude Desktop / Claude Code:**
```json
{
  "mcpServers": {
    "patchy": {
      "command": "/path/to/patchy",
      "env": {
        "PATCHY_SCOPE": "example.com"
      }
    }
  }
}
```

**SSE transport:**
```bash
PATCHY_SCOPE=example.com PATCHY_TRANSPORT=sse PATCHY_LISTEN=127.0.0.1:8080 ./bin/patchy
```

## Tools

### Primitives

| Tool | Description |
|------|-------------|
| `pd.subfinder.enumerate` | Passive subdomain enumeration |
| `pd.dnsx.resolve` | DNS resolution (A, AAAA, CNAME, MX, etc.) |
| `pd.httpx.probe` | HTTP probing with tech detection |
| `pd.naabu.scan` | TCP port scanning |
| `pd.katana.crawl` | Web crawling and endpoint discovery |
| `pd.nuclei.scan` | Template-based vulnerability scanning |

### Ecosystem

| Tool | Description |
|------|-------------|
| `pd.ecosystem.manifest` | Installed tool versions and health |
| `pd.ecosystem.doctor` | Health checks with actionable hints |
| `pd.ecosystem.update` | Update all tools and templates |
| `pd.ecosystem.setup` | Bootstrap full PD toolchain from scratch |

All primitives support **async task execution** — clients can call synchronously (default) or submit as background tasks with polling.

## Scope Enforcement

PATCHY is **deny-by-default**. No tool will execute against any target until scope is configured.

**Via environment variable:**
```bash
PATCHY_SCOPE="example.com,*.example.com,10.0.0.0/24"
```

Values containing `/` that parse as CIDR go to IP allowlists; everything else is treated as a domain pattern.

**Via config file:**
```yaml
policy:
  scope:
    allow_domains:
      - example.com
      - "*.example.com"
    allow_cidrs:
      - 10.0.0.0/24
    deny_domains:
      - internal.example.com
```

## Configuration

Config file search order:
1. `--config <path>` flag
2. `./patchy.yaml`
3. `~/.config/patchy/patchy.yaml`
4. `~/.patchy/patchy.yaml`

See [docs/configuration.md](docs/configuration.md) for the full schema.

**Environment overrides** — any setting can be overridden with `PATCHY_` prefix:

| Variable | Effect |
|----------|--------|
| `PATCHY_SCOPE` | Comma-separated allow targets |
| `PATCHY_TRANSPORT` | `stdio` or `sse` |
| `PATCHY_LISTEN` | SSE bind address |
| `PATCHY_LOG_LEVEL` | `debug`, `info`, `warn`, `error` |
| `PATCHY_BASE_DIR` | Artifact storage directory |
| `PATCHY_BINARY_PATH` | PD tool binary search path |

## Output Format

Every tool returns a `RunResult` envelope:

```json
{
  "schema_version": "1",
  "run_id": "550e8400-...",
  "tool": "pd.subfinder.enumerate",
  "status": "success",
  "timing": { "start": "...", "end": "...", "duration_ms": 4230 },
  "result": {
    "record_type": "SubfinderRecord",
    "count": 42,
    "records": [ ... ]
  }
}
```

Status values: `success`, `error`, `timeout`, `cancelled`, `policy_denied`.

Errors include actionable `hint` fields:
```json
{
  "error": {
    "code": "BINARY_NOT_FOUND",
    "message": "subfinder not found",
    "hint": "Install: pdtm -install subfinder, or call pd.ecosystem.setup"
  }
}
```

## Build & Test

```bash
make build          # Build binary
make test           # Unit tests (fast, no binaries needed)
make vet            # Static analysis
make doctor         # Health check

# Integration tests (requires PD binaries + network)
go test -tags integration -v -timeout 10m ./test/integration/
```

## Architecture

```
MCP Client
    |  JSON-RPC (stdio/SSE)
MCPServer (mcpbridge)
    |
Tool Handlers ── Policy Engine ── Runner ── PD Binary
    |                                |
    |                            BoundedBuffer
    v                                |
RunResult envelope              JSONL parser
    |
Artifact Store
```

**Key invariants:**
- No shell execution — `exec.Command` directly, never `sh -c`
- Binary allowlist — only registered binaries can execute
- Scope enforcement — every target checked before execution
- Bounded output — stdout/stderr capped to prevent OOM
- No silent failure — all errors surfaced with codes and hints

## Project Structure

```
cmd/patchy/          Entry point, CLI flags, transport dispatch
internal/
  config/            YAML config loading, env overrides, defaults
  mcpbridge/         MCP SDK abstraction (only package importing mcp-go)
  observability/     Structured slog logger, in-process metrics
  pipeline/          Multi-tool pipeline composition
  policy/            Scope, rate limits, concurrency, flag blocklists
  registry/          Binary discovery, health checks, manifest
  runner/            Sandboxed process execution, BoundedBuffer
  store/             Filesystem artifact store
  tools/             MCP tool handlers (one file per tool)
  update/            pdtm update state machine
pkg/schema/          Public types: RunResult, per-tool record structs
test/integration/    Full lifecycle integration tests
docs/                Configuration reference, architecture, setup guides
```

## Documentation

- [Setup Guide](docs/setup.md) — Install, configure for Claude Code / Windsurf / Cursor / Claude Desktop
- [Configuration Reference](docs/configuration.md) — Full YAML schema, env vars, policy defaults
- [Tool Reference](docs/tools.md) — All 10 tools with parameters, output types, examples
- [Architecture](docs/architecture.md) — Data flow, package boundaries, security model

## License

MIT — see [LICENSE](LICENSE) file.
