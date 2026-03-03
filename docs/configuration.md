# Configuration Reference

PATCHY loads configuration from YAML files and environment variables. Every setting has a sensible default — zero configuration starts a working server (though scope must be set for tools to accept targets).

## Config File Locations

Searched in order, first match wins:

1. `--config <path>` CLI flag
2. `./patchy.yaml` (current directory)
3. `./patchy.yml`
4. `~/.config/patchy/patchy.yaml`
5. `~/.patchy/patchy.yaml`
6. `/etc/patchy/patchy.yaml`

## Full Schema

```yaml
server:
  name: "patchy"                    # MCP server name
  version: "0.1.0"                  # Reported version
  transport: "stdio"                # stdio | sse
  listen: ":8080"                   # Bind address for SSE transport

binary:
  search_path: "~/.pdtm/go/bin"    # Where to find PD tool binaries
  pdtm_path: ""                     # Explicit pdtm path override
  overrides:                        # Per-tool binary path overrides
    httpx: "/usr/local/bin/httpx"   # Useful when multiple httpx versions exist

policy:
  scope:
    allow_domains:                  # Domains to allow (exact or wildcard)
      - example.com
      - "*.example.com"
      - ".example.com"             # Same as *.example.com
    allow_cidrs:                   # IP ranges to allow
      - 10.0.0.0/8
      - 192.168.1.0/24
    allow_urls:                    # URL prefix allowlist
      - https://example.com/api
    deny_domains:                  # Explicit deny (overrides allow)
      - internal.example.com
    deny_cidrs:
      - 10.0.0.1/32

  rate_limits:
    defaults:
      requests_per_min: 30
      burst: 5
    overrides:                     # Per-tool overrides
      naabu:
        requests_per_min: 10
        burst: 2

  concurrency:
    defaults:
      max_concurrent: 3
    overrides:
      nuclei:
        max_concurrent: 2

  timeouts:
    defaults:
      default: "5m"               # Default execution timeout
      max: "30m"                  # Maximum allowed timeout
    overrides:
      nuclei:
        default: "15m"
        max: "2h"

  naabu:
    allow_syn_scan: false          # SYN scan requires explicit opt-in

  nuclei:
    allow_interactsh: false        # Interactsh requires explicit opt-in

runner:
  max_stdout: "10MB"              # Stdout capture limit
  max_stderr: "1MB"               # Stderr capture limit
  default_timeout: "5m"           # Fallback timeout
  base_output_dir: "~/.patchy"    # Run artifact directory

store:
  base_dir: "~/.patchy"           # Artifact store root
  retention:
    runs: "7d"
    pipelines: "30d"
    updates: "90d"

logging:
  level: "info"                   # debug | info | warn | error
  format: "json"                  # json | text
  output: "stderr"                # stderr | file | both
```

## Environment Variables

Every setting can be overridden via environment variable. These take precedence over config file values.

| Variable | Maps To | Example |
|----------|---------|---------|
| `PATCHY_SCOPE` | `policy.scope.allow_domains` + `allow_cidrs` | `example.com,10.0.0.0/8` |
| `PATCHY_TRANSPORT` | `server.transport` | `sse` |
| `PATCHY_LISTEN` | `server.listen` | `127.0.0.1:8080` |
| `PATCHY_LOG_LEVEL` | `logging.level` | `debug` |
| `PATCHY_LOG_FORMAT` | `logging.format` | `text` |
| `PATCHY_BASE_DIR` | `store.base_dir` + `runner.base_output_dir` | `/tmp/patchy` |
| `PATCHY_BINARY_PATH` | `binary.search_path` | `/usr/local/bin` |

### PATCHY_SCOPE

The most important env var. Comma-separated list of targets to allow:

```bash
# Domains only
PATCHY_SCOPE="example.com,*.target.org"

# CIDRs only
PATCHY_SCOPE="10.0.0.0/8,192.168.1.0/24"

# Mixed (values with / that parse as CIDR → allow_cidrs, rest → allow_domains)
PATCHY_SCOPE="example.com,10.0.0.0/8,target.org"
```

`PATCHY_SCOPE` **appends** to config file scope — it does not replace it. This allows a base config to define deny rules while the env var sets the current engagement targets.

## Scope Patterns

### Domain Matching

| Pattern | Matches |
|---------|---------|
| `example.com` | Exactly `example.com` |
| `*.example.com` | `sub.example.com`, `deep.sub.example.com`, and `example.com` |
| `.example.com` | Same as `*.example.com` |

### CIDR Matching

Standard CIDR notation. IPv4 and IPv6 supported.

### Deny Overrides Allow

If a target matches both an allow and deny rule, **deny wins**. This lets you allow a broad domain while excluding sensitive subdomains:

```yaml
policy:
  scope:
    allow_domains: ["*.example.com"]
    deny_domains: ["internal.example.com", "admin.example.com"]
```

### Deny-by-Default

When no scope is configured (no allow_domains, allow_cidrs, or allow_urls), **all targets are denied**. This is a safety measure — you must explicitly declare your scope.

## Policy Defaults

### Rate Limits (MCP invocations per minute)

| Tool | Requests/min | Burst |
|------|-------------|-------|
| subfinder | 30 | 5 |
| dnsx | 60 | 10 |
| httpx | 30 | 5 |
| naabu | 10 | 2 |
| katana | 20 | 3 |
| nuclei | 10 | 2 |

### Concurrency (max parallel executions)

| Tool | Max Concurrent |
|------|---------------|
| subfinder | 3 |
| dnsx | 5 |
| httpx | 3 |
| naabu | 2 |
| katana | 2 |
| nuclei | 2 |

### Timeouts

| Tool | Default | Maximum |
|------|---------|---------|
| subfinder | 5m | 30m |
| dnsx | 2m | 10m |
| httpx | 5m | 30m |
| naabu | 10m | 30m |
| katana | 10m | 1h |
| nuclei | 15m | 2h |

### Blocked Flags

These flags are never passed through to PD tools regardless of input:

- **All tools:** `-update`, `-self-update`
- **nuclei:** `-code`, `-dast`, `-fuzz`, `-dast-server`, `-allow-local-file-access`

### Tool-Specific Policy

- **naabu:** Defaults to TCP connect scan. SYN scan requires `policy.naabu.allow_syn_scan: true`.
- **nuclei:** Interactsh disabled by default. Enable with `policy.nuclei.allow_interactsh: true`.

## CLI Flags

```
patchy [flags]

  --config <path>   Path to config file
  --version         Print version and exit
  --manifest        Print tool manifest (JSON) and exit
  --doctor          Run health checks and exit
```
