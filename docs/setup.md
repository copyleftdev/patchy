# Setup Guide

## Install PATCHY

### From source (recommended)

```bash
# Clone and install to $GOPATH/bin (usually ~/go/bin)
git clone https://github.com/patchy-mcp/patchy.git
cd patchy
go install ./cmd/patchy

# Verify
patchy --version
```

Make sure `$GOPATH/bin` (or `~/go/bin`) is in your `PATH`.

### Build locally

```bash
cd patchy
make build        # Outputs to ./bin/patchy
```

## Verify Installation

```bash
# Check what's installed (no scope needed for doctor)
patchy --doctor

# Check with scope
PATCHY_SCOPE=example.com patchy --doctor
```

Doctor will report the status of every component:
- pdtm (package manager)
- All 6 PD tools (subfinder, dnsx, httpx, naabu, katana, nuclei)
- Nuclei templates
- Output directory
- Scope configuration

Every failing check prints a `->` hint with the exact fix command.

## Install PD Tools

### Option A: Automatic (via MCP)

Connect PATCHY to your editor (see below), then call:

```
pd.ecosystem.setup
```

This installs pdtm, all PD tools, and nuclei templates in one step.

### Option B: Manual

```bash
# Install pdtm (ProjectDiscovery tool manager)
go install github.com/projectdiscovery/pdtm/cmd/pdtm@latest

# Install all tools
pdtm -install-all

# Install nuclei templates
nuclei -update-templates
```

---

## Configure for Claude Code

### Quick setup (CLI)

```bash
# Global — available in all projects
claude mcp add --transport stdio --scope user patchy -- /path/to/patchy

# With scope for a specific engagement
claude mcp add --transport stdio --scope user \
  --env PATCHY_SCOPE="target.com,*.target.com" \
  patchy -- /path/to/patchy

# Project-local — only this project
claude mcp add --transport stdio --scope project patchy -- /path/to/patchy
```

### Manual config

Add to `~/.claude.json` (user scope) or `.mcp.json` (project scope):

```json
{
  "mcpServers": {
    "patchy": {
      "type": "stdio",
      "command": "/home/user/go/bin/patchy",
      "args": [],
      "env": {
        "PATCHY_SCOPE": "target.com,*.target.com,10.0.0.0/24"
      }
    }
  }
}
```

### Verify in Claude Code

After restarting Claude Code, run:

```
/mcp
```

You should see `patchy` listed with 10 tools. Test with:

```
Call pd.ecosystem.doctor
```

### Usage

Once connected, Claude Code can call any PATCHY tool directly:

```
Enumerate subdomains for example.com
Probe example.com for HTTP services
Scan example.com for vulnerabilities with nuclei
```

PATCHY enforces scope — Claude cannot scan targets outside your configured allowlist.

---

## Configure for Windsurf

### Config file location

Edit `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "patchy": {
      "command": "/home/user/go/bin/patchy",
      "args": [],
      "env": {
        "PATCHY_SCOPE": "target.com,*.target.com"
      },
      "disabled": false
    }
  }
}
```

### Verify in Windsurf

1. Restart Windsurf (or reload window)
2. Open Cascade (Cmd/Ctrl+L)
3. Check the MCP icon — patchy should appear with its tools
4. Ask Cascade: `Run pd.ecosystem.doctor`

### Usage

Windsurf's Cascade agent can invoke PATCHY tools through natural language:

```
Find all subdomains of example.com using subfinder
Resolve DNS records for example.com
Check which ports are open on example.com
Crawl example.com and find all endpoints
Run nuclei security scan on example.com
```

---

## Configure for Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "patchy": {
      "command": "/home/user/go/bin/patchy",
      "args": [],
      "env": {
        "PATCHY_SCOPE": "target.com,*.target.com"
      }
    }
  }
}
```

Restart Claude Desktop. PATCHY tools appear in the tool list.

---

## Configure for Cursor

Edit `.cursor/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "patchy": {
      "command": "/home/user/go/bin/patchy",
      "args": [],
      "env": {
        "PATCHY_SCOPE": "target.com"
      }
    }
  }
}
```

---

## Scope Configuration

**PATCHY is deny-by-default.** No tool executes without scope.

### Via environment variable (simplest)

```bash
# Single domain
PATCHY_SCOPE=target.com

# Multiple targets
PATCHY_SCOPE="target.com,*.target.com,10.0.0.0/24"

# In MCP config env block
"env": { "PATCHY_SCOPE": "target.com,*.target.com" }
```

Values with `/` that parse as CIDR go to IP allowlists. Everything else is treated as a domain pattern.

### Via config file (full control)

Create `~/.config/patchy/patchy.yaml`:

```yaml
policy:
  scope:
    allow_domains:
      - target.com
      - "*.target.com"
    allow_cidrs:
      - 10.0.0.0/8
    deny_domains:
      - internal.target.com
```

Point PATCHY at it:

```json
{
  "mcpServers": {
    "patchy": {
      "command": "/home/user/go/bin/patchy",
      "args": ["--config", "/home/user/.config/patchy/patchy.yaml"]
    }
  }
}
```

### Changing scope between engagements

The env var approach is easiest — just update `PATCHY_SCOPE` in your MCP config and restart the editor. `PATCHY_SCOPE` appends to config file scope, so you can have a base deny list in the config file and set current targets via env.

---

## SSE Transport (Remote)

For shared/remote setups, PATCHY can serve over HTTP with Server-Sent Events:

```bash
PATCHY_TRANSPORT=sse PATCHY_LISTEN=127.0.0.1:8080 PATCHY_SCOPE=target.com patchy
```

Then configure MCP clients to connect via URL:

```json
{
  "mcpServers": {
    "patchy": {
      "serverUrl": "http://127.0.0.1:8080/sse"
    }
  }
}
```

---

## Troubleshooting

### "BINARY_NOT_FOUND" errors

Tool not installed. Run `patchy --doctor` to see which tools are missing, then either:
- Call `pd.ecosystem.setup` via MCP
- Run `pdtm -install <tool>` manually

### "NO_SCOPE" / "SCOPE_VIOLATION" errors

Scope not configured or target not in allowlist.
- Set `PATCHY_SCOPE` in your MCP config's env block
- Or create a `patchy.yaml` with `policy.scope.allow_domains`

### Doctor shows tools but MCP doesn't work

Check that the binary path in your MCP config is correct and absolute. Try running the exact command from your config in a terminal.

### Windsurf doesn't show tools

- Verify `~/.codeium/windsurf/mcp_config.json` is valid JSON
- Restart Windsurf completely (not just reload)
- Check Windsurf's output panel for MCP connection errors

### Claude Code doesn't show tools

- Run `claude mcp list` to verify patchy is registered
- Run `/mcp` inside Claude Code to see connection status
- Check `~/.claude.json` for the mcpServers entry
