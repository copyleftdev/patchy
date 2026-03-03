package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/config"
	"github.com/patchy-mcp/patchy/internal/mcpbridge"
	"github.com/patchy-mcp/patchy/internal/observability"
	"github.com/patchy-mcp/patchy/internal/policy"
	"github.com/patchy-mcp/patchy/internal/registry"
	"github.com/patchy-mcp/patchy/internal/runner"
	"github.com/patchy-mcp/patchy/internal/store"
	"github.com/patchy-mcp/patchy/internal/tools"
	"github.com/patchy-mcp/patchy/internal/update"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	configPath := flag.String("config", "", "Path to config file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	showManifest := flag.Bool("manifest", false, "Print tool manifest and exit")
	runDoctor := flag.Bool("doctor", false, "Run health checks and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("patchy %s (commit=%s, built=%s)\n", version, commit, date)
		os.Exit(0)
	}

	// 1. Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize logger
	logCloser := observability.NewLogger(cfg.Logging)
	defer logCloser.Close()
	logger := logCloser.Logger

	// 3. Initialize registry
	reg := registry.New(registry.BinaryConfig{
		SearchPath: cfg.Binary.SearchPath,
		PdtmPath:   cfg.Binary.PdtmPath,
		Overrides:  cfg.Binary.Overrides,
	}, logger)

	ctx := context.Background()
	if err := reg.Refresh(ctx); err != nil {
		logger.Warn("registry_refresh_partial", "error", err)
	}

	// Handle --manifest early (no policy needed)
	if *showManifest {
		manifest := reg.GetManifest()
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		os.Exit(0)
	}

	// 4. Initialize store
	baseDir := cfg.Store.BaseDir
	if baseDir == "" {
		baseDir = cfg.Runner.BaseOutputDir
	}
	artifactStore, err := store.NewFSStore(baseDir, logger)
	if err != nil {
		logger.Error("store_init_failed", "error", err)
		os.Exit(1)
	}

	// 5. Initialize policy engine
	policyEngine := policy.New(cfg.Policy, logger)

	// Handle --doctor after policy engine is initialized
	if *runDoctor {
		exitCode := tools.RunDoctorCLI(reg, policyEngine, artifactStore.BaseDir())
		os.Exit(exitCode)
	}

	// 6. Initialize runner
	r := runner.New(runner.Config{
		AllowedBinaries: reg.GetAllowedBinaries(),
		BaseOutputDir:   artifactStore.BaseDir(),
		MaxStdout:       config.ParseSize(cfg.Runner.MaxStdout, 10<<20),
		MaxStderr:       config.ParseSize(cfg.Runner.MaxStderr, 1<<20),
		DefaultTimeout:  config.ParseDuration(cfg.Runner.DefaultTimeout, 0),
	}, logger)

	// 7. Initialize update controller
	updateCtrl := update.NewController(reg, r, policyEngine, artifactStore, logger)

	// 8. Create MCP server
	mcpServer := mcpbridge.NewServer(mcpbridge.ServerConfig{
		Name:      cfg.Server.Name,
		Version:   cfg.Server.Version,
		Transport: cfg.Server.Transport,
		Listen:    cfg.Server.Listen,
	})

	// 9. Register all tools
	deps := tools.Deps{
		Runner:   r,
		Policy:   policyEngine,
		Registry: reg,
		Logger:   logger,
		BaseDir:  artifactStore.BaseDir(),
	}
	tools.RegisterAll(mcpServer, deps)
	tools.RegisterUpdate(mcpServer, updateCtrl)
	tools.RegisterSetup(mcpServer, deps, updateCtrl)

	// 10. Save initial manifest
	_ = artifactStore.SaveManifest(reg.GetManifest())

	// 11. Log startup health summary
	logStartupSummary(logger, reg, policyEngine, cfg)

	if err := serveTransport(mcpServer, cfg.Server, logger); err != nil {
		logger.Error("server_error", "error", err)
		os.Exit(1)
	}
}

func logStartupSummary(logger *slog.Logger, reg *registry.Registry, pe *policy.Engine, cfg config.Config) {
	manifest := reg.GetManifest()

	var installed, missing []string
	for _, t := range manifest.Tools {
		if t.Healthy {
			installed = append(installed, t.Name)
		} else {
			missing = append(missing, t.Name)
		}
	}

	pdtmStatus := "not found"
	if manifest.Pdtm != nil && manifest.Pdtm.Installed {
		pdtmStatus = "installed (" + manifest.Pdtm.Version + ")"
	}

	templatesStatus := "not found"
	if manifest.Templates != nil && manifest.Templates.Version != "" {
		templatesStatus = "installed (" + manifest.Templates.Version + ")"
	}

	scopeStatus := "not configured"
	if pe.IsScopeConfigured() {
		scopeStatus = "configured"
	}

	logger.Info("server_start",
		"component", "main",
		"name", cfg.Server.Name,
		"version", cfg.Server.Version,
		"transport", cfg.Server.Transport,
		"tools_installed", strings.Join(installed, ","),
		"tools_missing", strings.Join(missing, ","),
		"pdtm", pdtmStatus,
		"templates", templatesStatus,
		"scope", scopeStatus,
	)

	if len(missing) > 0 {
		logger.Warn("tools_missing",
			"component", "main",
			"missing", strings.Join(missing, ","),
			"hint", "Run pd.ecosystem.setup to install missing tools, or: pdtm -install-all",
		)
	}

	if scopeStatus == "not configured" {
		logger.Warn("no_scope_configured",
			"component", "main",
			"hint", "Set PATCHY_SCOPE=<target> or configure policy.scope in patchy.yaml; all targets will be denied until scope is set",
		)
	}
}

func serveTransport(s *server.MCPServer, cfg config.ServerConfig, logger *slog.Logger) error {
	switch cfg.Transport {
	case "stdio", "":
		return server.ServeStdio(s)
	case "sse":
		sseServer := server.NewSSEServer(s)
		listen := cfg.Listen
		if listen == "" {
			listen = ":8080"
		}
		logger.Info("sse_listen", "address", listen)
		return sseServer.Start(listen)
	default:
		return fmt.Errorf("unsupported transport: %s", cfg.Transport)
	}
}
