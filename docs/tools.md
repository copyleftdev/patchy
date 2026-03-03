# Tool Reference

Every tool returns a [`RunResult`](#output-format) envelope with consistent structure. All primitives support async task execution.

## Primitives

### pd.subfinder.enumerate

Passive subdomain enumeration using multiple sources.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `domain` | string | yes | Target domain |
| `sources` | string[] | no | Specific sources (crtsh, hackertarget, etc.) |
| `rate_limit` | number | no | Requests per second |
| `timeout` | number | no | Timeout in minutes (default 5) |
| `recursive` | boolean | no | Recursive subdomain enumeration |
| `all` | boolean | no | Use all sources including slow ones |

**Record type:** `SubfinderRecord`

```json
{ "host": "sub.example.com", "input": "example.com", "source": "crtsh" }
```

**Example:**
```json
{
  "name": "pd.subfinder.enumerate",
  "arguments": {
    "domain": "example.com",
    "all": true,
    "timeout": 10
  }
}
```

---

### pd.dnsx.resolve

DNS resolution with support for multiple query types.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `hosts` | string[] | yes | Hostnames to resolve |
| `query_types` | string[] | no | DNS types: a, aaaa, cname, mx, ns, txt, soa, ptr, srv, caa |
| `show_cdn` | boolean | no | Show CDN provider |
| `show_asn` | boolean | no | Show ASN information |
| `threads` | number | no | Number of threads |
| `rate_limit` | number | no | Requests per second |
| `trace` | boolean | no | Enable DNS trace |

**Record type:** `DnsxRecord`

```json
{
  "host": "example.com",
  "a": ["93.184.216.34"],
  "resolver": ["8.8.8.8:53"]
}
```

**Example:**
```json
{
  "name": "pd.dnsx.resolve",
  "arguments": {
    "hosts": ["example.com", "www.example.com"],
    "query_types": ["a", "aaaa", "mx"]
  }
}
```

---

### pd.httpx.probe

HTTP probing with technology detection, status codes, and response metadata.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `targets` | string[] | yes | URLs or hosts to probe |
| `ports` | string | no | Ports to probe (e.g., "80,443,8080") |
| `paths` | string[] | no | URL paths to append |
| `method` | string | no | HTTP method |
| `probes` | string[] | no | Data to extract: status_code, title, server, tech_detect, content_type, ip, cname, asn, cdn, jarm |
| `follow_redirects` | boolean | no | Follow redirects |
| `max_redirects` | number | no | Maximum redirect depth |
| `match_codes` | string | no | Only return these status codes |
| `filter_codes` | string | no | Exclude these status codes |
| `threads` | number | no | Number of threads |
| `rate_limit` | number | no | Requests per second |
| `timeout` | number | no | Timeout in seconds |
| `tls_grab` | boolean | no | Grab TLS certificate info |

**Record type:** `HttpxRecord`

```json
{
  "url": "https://example.com",
  "status_code": 200,
  "title": "Example Domain",
  "webserver": "ECS",
  "technologies": ["Amazon ECS"]
}
```

---

### pd.naabu.scan

TCP port scanning with configurable scan types.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `hosts` | string[] | yes | Hosts or IPs to scan |
| `ports` | string | no | Ports to scan (e.g., "80,443,1000-2000") |
| `top_ports` | string | no | Scan top N ports: "100", "1000" |
| `exclude_ports` | string | no | Ports to exclude |
| `scan_type` | string | no | Scan type: connect (default) or s (SYN, requires policy opt-in) |
| `rate` | number | no | Packets per second |
| `workers` | number | no | Number of workers |
| `timeout` | number | no | Timeout in milliseconds |
| `passive` | boolean | no | Passive mode (no active scanning) |
| `exclude_cdn` | boolean | no | Exclude CDN IPs |

**Record type:** `NaabuRecord`

```json
{ "host": "example.com", "ip": "93.184.216.34", "port": 443, "protocol": "tcp" }
```

**Note:** SYN scan requires `policy.naabu.allow_syn_scan: true` in configuration.

---

### pd.katana.crawl

Web crawling and endpoint discovery with JavaScript parsing support.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `targets` | string[] | yes | URLs to crawl |
| `depth` | number | no | Crawl depth (default 3) |
| `js_crawl` | boolean | no | Enable JavaScript crawling |
| `strategy` | string | no | Crawl strategy: depth-first, breadth-first |
| `crawl_duration` | string | no | Maximum crawl duration |
| `scope_regex` | string | no | Regex to constrain crawl scope |
| `concurrency` | number | no | Number of concurrent crawlers |
| `rate_limit` | number | no | Requests per second |
| `timeout` | number | no | Timeout in seconds |
| `headless` | boolean | no | Use headless browser |
| `form_extraction` | boolean | no | Extract form data |

**Record type:** `KatanaRecord`

```json
{
  "url": "https://example.com/page",
  "path": "/page",
  "fqdn": "example.com",
  "source": "https://example.com",
  "tag": "a",
  "attribute": "href"
}
```

---

### pd.nuclei.scan

Template-based vulnerability scanning with severity filtering.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `targets` | string[] | yes | URLs or hosts to scan |
| `tags` | string[] | no | Template tags: cve, xss, sqli, etc. |
| `severity` | string[] | no | Severity filter: info, low, medium, high, critical |
| `templates` | string[] | no | Specific template IDs |
| `exclude_tags` | string[] | no | Tags to exclude |
| `rate_limit` | number | no | Requests per second |
| `concurrency` | number | no | Concurrent template executions |
| `new_templates` | boolean | no | Only run newly added templates |
| `automatic_scan` | boolean | no | Map technologies to templates automatically |

**Record type:** `NucleiRecord`

```json
{
  "template_id": "cve-2021-44228",
  "host": "https://example.com",
  "matched_at": "https://example.com/api",
  "info": {
    "name": "Apache Log4j RCE",
    "severity": "critical",
    "tags": ["cve", "rce", "log4j"]
  }
}
```

**Safety:** Unsigned templates are always disabled. Interactsh is disabled by default. Flags `-code`, `-dast`, `-fuzz` are blocked.

---

## Ecosystem Tools

### pd.ecosystem.manifest

Returns the current manifest of installed tools, versions, and health status. No parameters.

**Returns:** JSON manifest with tool entries, pdtm status, template info, and timestamps.

### pd.ecosystem.doctor

Run health checks on all components with actionable remediation hints.

**Returns:** Array of checks (pass/fail/warn) with hints for fixing issues.

**Check categories:**
- pdtm installation
- Each PD tool binary (installed, healthy, version)
- Nuclei templates
- Output directory writability
- Scope configuration

### pd.ecosystem.update

Update all PD tools and nuclei templates.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `tools` | string[] | no | Specific tools to update (empty = all) |
| `include_templates` | boolean | no | Update nuclei templates (default true) |
| `include_pdtm` | boolean | no | Self-update pdtm (default true) |
| `dry_run` | boolean | no | Preview without changes |

**Returns:** Update result with phase outcomes and manifest diff.

### pd.ecosystem.setup

Bootstrap the full PD tool ecosystem from scratch.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `skip_templates` | boolean | no | Skip nuclei template download (default false) |

**Phases:**
1. Install pdtm via `go install` (if missing)
2. Install all PD tools via `pdtm -install-all`
3. Download nuclei templates (unless skipped)

**Returns:** Setup result with phase outcomes and before/after manifest diff.

Supports async task execution — use task params for non-blocking setup.

---

## Output Format

Every tool returns a `RunResult` envelope:

```json
{
  "schema_version": "1",
  "run_id": "550e8400-e29b-41d4-a716-446655440000",
  "tool": "pd.subfinder.enumerate",
  "tool_version": "2.6.3",
  "binary_path": "/home/user/.pdtm/go/bin/subfinder",
  "status": "success",
  "invocation": {
    "args": ["-d", "example.com", "-json", "-duc", "-nc"],
    "cwd": "/home/user/.patchy/runs/550e8400..."
  },
  "timing": {
    "start": "2025-01-15T10:30:00Z",
    "end": "2025-01-15T10:30:04Z",
    "duration_ms": 4230
  },
  "result": {
    "record_type": "SubfinderRecord",
    "count": 42,
    "records": [ ... ],
    "stdout": "...",
    "stderr": "...",
    "truncated": false
  },
  "error": null,
  "environment": {
    "patchy_version": "0.1.0",
    "tool_version": "2.6.3",
    "os": "linux",
    "arch": "amd64"
  }
}
```

### Status Values

| Status | Meaning |
|--------|---------|
| `success` | Tool executed and exited 0 |
| `error` | Execution failed |
| `timeout` | Killed after timeout |
| `cancelled` | Client disconnected |
| `policy_denied` | Policy engine rejected the request |

### Error Codes

| Code | Source | Meaning |
|------|--------|---------|
| `BINARY_NOT_FOUND` | Runner | Tool binary not installed |
| `BINARY_NOT_ALLOWED` | Runner | Binary not in allowlist |
| `EXECUTION_FAILED` | Runner | Non-zero exit code |
| `EXECUTION_TIMEOUT` | Runner | Timeout exceeded |
| `EXECUTION_CANCELLED` | Runner | Context cancelled |
| `NO_SCOPE` | Policy | No scope configured |
| `SCOPE_VIOLATION` | Policy | Target not in scope |
| `RATE_LIMIT` | Policy | Rate limit exceeded |
| `CONCURRENCY_LIMIT` | Policy | Max concurrent reached |
| `BLOCKED_FLAG` | Policy | Forbidden flag detected |
| `UPDATE_IN_PROGRESS` | Policy | Tools being updated |
| `INVALID_INPUT` | Handler | Missing or invalid parameters |

All errors include a `hint` field with actionable remediation guidance.
