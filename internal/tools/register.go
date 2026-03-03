package tools

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/update"
)

// RegisterAll registers all MCP tools on the server.
func RegisterAll(s *server.MCPServer, deps Deps) {
	RegisterEcosystem(s, deps)
	RegisterPrimitives(s, deps)
}

// RegisterEcosystem registers pd.ecosystem.* tools.
func RegisterEcosystem(s *server.MCPServer, deps Deps) {
	registerEcosystem(s, deps, deps.BaseDir)
}

// RegisterPrimitives registers all pd.<tool>.* primitive tools.
func RegisterPrimitives(s *server.MCPServer, deps Deps) {
	registerSubfinder(s, deps)
	registerDnsx(s, deps)
	registerHttpx(s, deps)
	registerNaabu(s, deps)
	registerKatana(s, deps)
	registerNuclei(s, deps)
}

// RegisterSetup registers the pd.ecosystem.setup tool.
func RegisterSetup(s *server.MCPServer, deps Deps, updateCtrl *update.Controller) {
	registerSetup(s, deps, updateCtrl)
}
