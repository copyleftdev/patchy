package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
	"github.com/patchy-mcp/patchy/internal/policy"
	"github.com/patchy-mcp/patchy/internal/registry"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

// DoctorCheck is a single health check result.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass, fail, warn
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func registerEcosystem(s *server.MCPServer, deps Deps, baseDir string) {
	registerManifest(s, deps)
	registerDoctor(s, deps, baseDir)
}

func registerManifest(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("pd.ecosystem.manifest",
		mcp.WithDescription("Returns the current manifest of installed PD tools, versions, and health status."),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		manifest := deps.Registry.GetManifest()
		manifest.PatchyVersion = patchyVersion

		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return mcpbridge.NewSimpleError("INTERNAL_ERROR", "failed to marshal manifest: "+err.Error()), nil
		}

		return mcpbridge.NewTextResult(string(data)), nil
	})
}

func registerDoctor(s *server.MCPServer, deps Deps, baseDir string) {
	tool := mcp.NewTool("pd.ecosystem.doctor",
		mcp.WithDescription("Run health checks on all PD tool binaries and report issues with actionable hints."),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = deps.Registry.Refresh(ctx)
		manifest := deps.Registry.GetManifest()

		checks := runDoctorChecks(manifest, deps.Policy, baseDir)

		allPass := true
		var summary []string
		for _, c := range checks {
			if c.Status == "fail" {
				allPass = false
			}
			line := fmt.Sprintf("[%s] %s: %s", strings.ToUpper(c.Status), c.Name, c.Message)
			if c.Hint != "" {
				line += "\n  -> " + c.Hint
			}
			summary = append(summary, line)
		}

		result := map[string]interface{}{
			"checks":  checks,
			"healthy": allPass,
			"summary": strings.Join(summary, "\n"),
		}

		data, _ := json.MarshalIndent(result, "", "  ")
		return mcpbridge.NewTextResult(string(data)), nil
	})
}

// runDoctorChecks runs all health checks and returns results with hints.
func runDoctorChecks(manifest *schema.Manifest, policyEngine *policy.Engine, baseDir string) []DoctorCheck {
	var checks []DoctorCheck

	// Check pdtm
	if manifest.Pdtm != nil && manifest.Pdtm.Installed {
		checks = append(checks, DoctorCheck{
			Name:    "pdtm",
			Status:  "pass",
			Message: fmt.Sprintf("pdtm %s at %s", manifest.Pdtm.Version, manifest.Pdtm.BinaryPath),
		})
	} else {
		checks = append(checks, DoctorCheck{
			Name:    "pdtm",
			Status:  "warn",
			Message: "pdtm not found (updates will not work)",
			Hint:    "Install: go install github.com/projectdiscovery/pdtm/cmd/pdtm@latest, or call pd.ecosystem.setup",
		})
	}

	// Check each tool
	for _, t := range manifest.Tools {
		if t.Healthy {
			checks = append(checks, DoctorCheck{
				Name:    t.Name,
				Status:  "pass",
				Message: fmt.Sprintf("%s %s", t.Name, t.Version),
			})
		} else if t.Installed {
			checks = append(checks, DoctorCheck{
				Name:    t.Name,
				Status:  "warn",
				Message: fmt.Sprintf("%s installed but unhealthy: %s", t.Name, t.Error),
				Hint:    fmt.Sprintf("Check binary permissions or reinstall: pdtm -install %s", t.Name),
			})
		} else {
			checks = append(checks, DoctorCheck{
				Name:    t.Name,
				Status:  "fail",
				Message: fmt.Sprintf("%s not found", t.Name),
				Hint:    fmt.Sprintf("Install: pdtm -install %s, or call pd.ecosystem.setup", t.Name),
			})
		}
	}

	// Check templates
	if manifest.Templates != nil && manifest.Templates.Version != "" {
		checks = append(checks, DoctorCheck{
			Name:    "nuclei-templates",
			Status:  "pass",
			Message: fmt.Sprintf("templates %s at %s", manifest.Templates.Version, manifest.Templates.Path),
		})
	} else {
		checks = append(checks, DoctorCheck{
			Name:    "nuclei-templates",
			Status:  "warn",
			Message: "nuclei templates not found or version unknown",
			Hint:    "Run: nuclei -update-templates, or call pd.ecosystem.update",
		})
	}

	// Check output directory
	if baseDir != "" {
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			checks = append(checks, DoctorCheck{
				Name:    "output_dir",
				Status:  "fail",
				Message: fmt.Sprintf("cannot create output dir %s: %v", baseDir, err),
			})
		} else {
			checks = append(checks, DoctorCheck{
				Name:    "output_dir",
				Status:  "pass",
				Message: fmt.Sprintf("output dir %s writable", baseDir),
			})
		}
	}

	// Check scope configuration
	if policyEngine != nil {
		if policyEngine.IsScopeConfigured() {
			checks = append(checks, DoctorCheck{
				Name:    "scope",
				Status:  "pass",
				Message: "scope configured",
			})
		} else {
			checks = append(checks, DoctorCheck{
				Name:    "scope",
				Status:  "warn",
				Message: "no scope configured; all targets will be denied",
				Hint:    "Set PATCHY_SCOPE=<target> or configure policy.scope in patchy.yaml",
			})
		}
	}

	return checks
}

// RunDoctorCLI runs the doctor check and prints results to stdout (for --doctor flag).
func RunDoctorCLI(reg *registry.Registry, policyEngine *policy.Engine, baseDir string) int {
	ctx := context.Background()
	_ = reg.Refresh(ctx)
	manifest := reg.GetManifest()

	checks := runDoctorChecks(manifest, policyEngine, baseDir)

	allPass := true
	for _, c := range checks {
		symbol := "✓"
		switch c.Status {
		case "fail":
			symbol = "✗"
			allPass = false
		case "warn":
			symbol = "!"
		}
		fmt.Printf("%s %s\n", symbol, c.Message)
		if c.Hint != "" && c.Status != "pass" {
			fmt.Printf("  -> %s\n", c.Hint)
		}
	}

	if allPass {
		fmt.Println("\nAll checks passed.")
		return 0
	}
	fmt.Println("\nSome checks failed.")
	return 1
}
