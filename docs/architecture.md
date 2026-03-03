# Architecture

PATCHY is a Go MCP server that mediates between LLM agents and ProjectDiscovery security tools. Every execution is scope-checked, sandboxed, rate-limited, and returns structured output.

## Data Flow

```
MCP Client (Claude, Cursor, etc.)
    |
    |  JSON-RPC over stdio or SSE
    v
MCPServer (mark3labs/mcp-go via mcpbridge/)
    |
    |  mcp.CallToolRequest
    v
Tool Handler (internal/tools/<tool>.go)
    |
    |  1. Parse + validate input
    |  2. Extract targets
    |  3. Build CLI args
    |
    |  policy.EvalRequest{targets, args}
    v
Policy Engine (internal/policy/)
    |  - Scope check (domain/CIDR allow/deny)
    |  - Rate limit (token bucket per tool)
    |  - Concurrency (semaphore per tool)
    |  - Flag blocklist
    |  - Timeout bounds
    |
    |  policy.EvalResult{allowed, clamped_timeout}
    v
Runner (internal/runner/)
    |  - Resolve binary path (allowlist check)
    |  - Create sandboxed working directory
    |  - exec.CommandContext (no shell, minimal env)
    |  - Capture stdout/stderr (BoundedBuffer)
    |  - Parse JSONL output
    |  - Enforce timeout (SIGTERM → SIGKILL)
    |
    |  runner.RunResult
    v
Tool Handler
    |  Package into schema.RunResult envelope
    v
MCPServer → MCP Client
    |
    v
Artifact Store (filesystem)
```

## Package Boundaries

### `cmd/patchy/`

Entry point. Wires all dependencies, parses CLI flags, starts transport. No business logic.

### `internal/mcpbridge/`

**The only package that imports `mcp-go`.** Provides:
- `NewServer()` — Creates configured MCP server
- `NewResult()`, `NewErrorResult()` — Builds MCP response from RunResult
- `NewTextResult()`, `NewSimpleError()` — Simple response builders

This abstraction exists so the MCP SDK can be swapped without touching any other package.

### `internal/tools/`

One file per PD tool handler. Every handler follows the same pattern:

1. Register MCP tool definition with input schema
2. Extract and validate params from request
3. Build CLI args from params
4. Call `executeTool()` (shared.go) which orchestrates policy → runner → packaging

`shared.go` contains `executeTool()` and all shared utilities (param extraction, flag builders, error mapping).

`ecosystem.go` contains manifest, doctor, and doctor check logic.
`setup.go` contains the bootstrap flow.
`update.go` wraps the update controller.

### `internal/policy/`

Stateless evaluation engine (except rate/concurrency counters). No knowledge of tools — operates on targets, args, and tool names.

**Evaluation chain:** update lock → scope → rate limit → concurrency → flags → tool-specific → timeout bounds.

**Key design:** Deny-by-default. If no scope is configured, all targets are rejected.

### `internal/runner/`

Tool-agnostic execution engine. Given a binary name and args, it:

1. Resolves the binary path from the allowlist
2. Creates `$base/runs/$run_id/` working directory
3. Builds `exec.Cmd` with minimal environment (HOME, USER, LANG — no PATH)
4. Starts process, captures output into `BoundedBuffer`
5. Waits for completion or timeout
6. Parses JSONL from stdout or output file

`BoundedBuffer` is a ring buffer that keeps the last N bytes and reports truncation. Default limits: 10 MB stdout, 1 MB stderr.

**Timeout handling:** SIGTERM, wait 5s grace period, then SIGKILL.

### `internal/registry/`

Discovers PD tool binaries on the filesystem. For each tool:
- Looks in search path (default: `~/.pdtm/go/bin`)
- Runs `<tool> -version` to get version string
- Tracks installed/healthy status

Provides `GetManifest()` (snapshot), `GetAllowedBinaries()` (for runner), and `Refresh()` (re-scan).

**Graceful degradation:** Missing tools don't block server startup. They return `BINARY_NOT_FOUND` when called.

### `internal/store/`

Filesystem artifact store rooted at `~/.patchy/`:

```
~/.patchy/
  runs/         Per-execution output files
  pipelines/    Pipeline results
  updates/      Update logs with before/after manifests
  manifests/    Manifest snapshots
```

Writes are atomic (temp file + rename).

### `internal/update/`

Update state machine:

1. **Lock** — Prevent concurrent updates, block tool execution via policy
2. **Pre-snapshot** — Capture current versions
3. **Update pdtm** — `pdtm -self-update`
4. **Update tools** — `pdtm -update-all` or `pdtm -install <tool>`
5. **Update templates** — `nuclei -update-templates`
6. **Post-snapshot** — Capture new versions
7. **Diff** — Compute `ManifestDiff`
8. **Refresh** — Hot-reload runner's binary allowlist

### `internal/pipeline/`

Composes multi-tool workflows. Predefined pipelines:

- **asset_discovery:** subfinder → dnsx
- **web_attack_surface:** httpx → katana
- **vuln_sweep:** nuclei against discovered targets

Each step's output is transformed into the next step's input (e.g., SubfinderRecord hosts → dnsx targets).

### `pkg/schema/`

**Public, stable types.** The only `pkg/` directory — everything here is part of the external contract.

- `RunResult` — Universal return envelope
- Per-tool record structs (SubfinderRecord, DnsxRecord, etc.)
- `Manifest`, `ManifestDiff` — Ecosystem state types

**Evolution rule:** Append-only. Fields are added, never removed or renamed.

## Security Model

### No Shell Execution

All process creation uses `exec.Command` directly. Arguments are separate string elements, never interpolated into a shell command. There is no `sh -c` anywhere in the codebase.

### Binary Allowlist

The runner maintains a name → path map of allowed binaries. Only binaries registered by the registry (discovered at startup or after refresh) can execute. The map is updated after ecosystem operations.

### Scope Enforcement

Every target string is checked against domain and CIDR allowlists before execution. Deny rules override allow rules. When no scope is configured, all targets are denied.

URL targets are decomposed: the host is extracted and checked against domain/CIDR rules.

### Minimal Environment

Runner processes inherit only `HOME`, `USER`, `LANG`, `LC_ALL`. No `PATH` — binaries are resolved to absolute paths at registration time.

### Resource Bounds

- **BoundedBuffer** caps stdout/stderr to prevent OOM (default 10 MB/1 MB)
- **Concurrency limits** prevent resource exhaustion (2-5 concurrent per tool)
- **Timeouts** with hard kill (SIGTERM → SIGKILL) prevent runaway processes
- **Rate limits** prevent excessive API/network usage

### Flag Blocklist

Dangerous flags are blocked regardless of input:
- Update flags (`-update`, `-self-update`) on all tools
- Code execution flags (`-code`, `-dast`, `-fuzz`) on nuclei
- Unsigned template flags on nuclei

## Observability

All logging uses `log/slog` (Go stdlib). Structured JSON by default.

**Standard fields:** `run_id`, `tool`, `component`, `ts`

**Key log events:**
- `exec_start`, `exec_complete` — Runner lifecycle
- `eval_pass`, `eval_deny` — Policy decisions
- `phase_start`, `phase_complete` — Update phases
- `server_start` — Startup summary with tool health

In-process counters track invocations, successes, failures, timeouts, denials, and duration distributions.

## Async Task Support

All 6 primitive tools are registered with `TaskSupportOptional`. Clients can:

1. **Synchronous (default):** Call tool, block until result
2. **Async:** Submit with task params, receive task ID, poll for completion

The MCP server manages task lifecycle: `created → working → completed/failed`.
