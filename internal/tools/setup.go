package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
	"github.com/patchy-mcp/patchy/internal/update"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

// SetupResult captures the outcome of the setup process.
type SetupResult struct {
	RunID  string             `json:"run_id"`
	Status string             `json:"status"`
	Phases []SetupPhase       `json:"phases"`
	Diff   *schema.ManifestDiff `json:"diff,omitempty"`
	Timing schema.TimingInfo  `json:"timing"`
}

// SetupPhase captures one step of the setup flow.
type SetupPhase struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // success, error, skipped
	Message string `json:"message,omitempty"`
}

func registerSetup(s *server.MCPServer, deps Deps, updateCtrl *update.Controller) {
	tool := mcp.NewTool("pd.ecosystem.setup",
		mcp.WithDescription("Bootstrap the full PD tool ecosystem: installs pdtm (if missing), all PD tools, and nuclei templates."),
		mcp.WithBoolean("skip_templates",
			mcp.Description("Skip nuclei template installation. Default false."),
		),
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := getArgs(req)
		skipTemplates, _ := getBool(params, "skip_templates")

		runID := uuid.New().String()
		logger := deps.Logger.With("run_id", runID, "tool", "pd.ecosystem.setup")
		start := time.Now()

		result := &SetupResult{
			RunID:  runID,
			Status: "running",
		}

		// 1. Pre-snapshot
		preManifest := deps.Registry.GetManifest()

		// 2. Bootstrap pdtm if missing
		if preManifest.Pdtm == nil || !preManifest.Pdtm.Installed {
			logger.Info("setup_phase", "phase", "bootstrap_pdtm")
			phase := bootstrapPdtm(ctx)
			result.Phases = append(result.Phases, phase)

			if phase.Status == "error" {
				result.Status = "error"
				result.Timing = setupTiming(start)
				data, _ := json.MarshalIndent(result, "", "  ")
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{mcp.NewTextContent(string(data))},
				}, nil
			}

			// Refresh registry so pdtm enters the allowlist
			_ = deps.Registry.Refresh(ctx)
			deps.Runner.UpdateAllowedBinaries(deps.Registry.GetAllowedBinaries())
		} else {
			result.Phases = append(result.Phases, SetupPhase{
				Name:    "bootstrap_pdtm",
				Status:  "skipped",
				Message: "pdtm already installed",
			})
		}

		// 3. Install all tools via pdtm
		logger.Info("setup_phase", "phase", "install_tools")
		if err := updateCtrl.InstallAllTools(ctx, runID); err != nil {
			result.Phases = append(result.Phases, SetupPhase{
				Name:    "install_tools",
				Status:  "error",
				Message: err.Error(),
			})
			result.Status = "error"
			result.Timing = setupTiming(start)
			data, _ := json.MarshalIndent(result, "", "  ")
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.NewTextContent(string(data))},
			}, nil
		}
		result.Phases = append(result.Phases, SetupPhase{
			Name:   "install_tools",
			Status: "success",
		})

		// Refresh registry after tool install
		_ = deps.Registry.Refresh(ctx)
		deps.Runner.UpdateAllowedBinaries(deps.Registry.GetAllowedBinaries())

		// 4. Install templates
		if !skipTemplates {
			logger.Info("setup_phase", "phase", "install_templates")
			if err := updateCtrl.UpdateTemplates(ctx, runID); err != nil {
				result.Phases = append(result.Phases, SetupPhase{
					Name:    "install_templates",
					Status:  "error",
					Message: "template install failed (non-fatal): " + err.Error(),
				})
			} else {
				result.Phases = append(result.Phases, SetupPhase{
					Name:   "install_templates",
					Status: "success",
				})
			}
		} else {
			result.Phases = append(result.Phases, SetupPhase{
				Name:    "install_templates",
				Status:  "skipped",
				Message: "skip_templates=true",
			})
		}

		// 5. Post-snapshot and diff
		_ = deps.Registry.Refresh(ctx)
		deps.Runner.UpdateAllowedBinaries(deps.Registry.GetAllowedBinaries())
		postManifest := deps.Registry.GetManifest()
		result.Diff = schema.Diff(preManifest, postManifest)

		result.Status = "success"
		result.Timing = setupTiming(start)

		logger.Info("setup_complete", "status", result.Status, "duration_ms", result.Timing.DurationMs)

		data, _ := json.MarshalIndent(result, "", "  ")
		return mcpbridge.NewTextResult(string(data)), nil
	})
}

// bootstrapPdtm installs pdtm via go install with a hardcoded command.
// This bypasses the runner because pdtm is not yet in the allowlist.
func bootstrapPdtm(ctx context.Context) SetupPhase {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return SetupPhase{
			Name:    "bootstrap_pdtm",
			Status:  "error",
			Message: "go not found in PATH; install Go first or install pdtm manually: https://github.com/projectdiscovery/pdtm",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, goPath, "install", "github.com/projectdiscovery/pdtm/cmd/pdtm@latest")
	cmd.Env = cleanGoEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return SetupPhase{
			Name:    "bootstrap_pdtm",
			Status:  "error",
			Message: fmt.Sprintf("go install pdtm failed: %v: %s", err, stderr.String()),
		}
	}

	return SetupPhase{
		Name:   "bootstrap_pdtm",
		Status: "success",
	}
}

// cleanGoEnv returns a minimal environment for go install.
func cleanGoEnv() []string {
	env := []string{
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"PATH=" + os.Getenv("PATH"),
		"LANG=en_US.UTF-8",
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		env = append(env, "GOPATH="+gopath)
	}
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		env = append(env, "GOBIN="+gobin)
	}
	return env
}

func setupTiming(start time.Time) schema.TimingInfo {
	end := time.Now()
	return schema.TimingInfo{
		Start:      start.Format(time.RFC3339),
		End:        end.Format(time.RFC3339),
		DurationMs: end.Sub(start).Milliseconds(),
	}
}
