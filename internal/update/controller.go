package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/patchy-mcp/patchy/internal/policy"
	"github.com/patchy-mcp/patchy/internal/registry"
	"github.com/patchy-mcp/patchy/internal/runner"
	"github.com/patchy-mcp/patchy/internal/store"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

// UpdateResult captures the full outcome of an update cycle.
type UpdateResult struct {
	RunID    string             `json:"run_id"`
	Status   string             `json:"status"` // success | partial | error
	Timing   schema.TimingInfo  `json:"timing"`
	Diff     *schema.ManifestDiff `json:"diff"`
	Phases   []PhaseResult      `json:"phases"`
	Warnings []string           `json:"warnings,omitempty"`
	Errors   []string           `json:"errors,omitempty"`
}

// PhaseResult captures one phase of the update.
type PhaseResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // success | warn | error | skipped
	DurationMs int64  `json:"duration_ms"`
	Message    string `json:"message,omitempty"`
}

// UpdateConfig holds options for a single update invocation.
type UpdateConfig struct {
	Tools            []string // specific tools to update; empty = all
	IncludeTemplates bool
	IncludePdtm      bool
	DryRun           bool
}

// Controller manages the update state machine.
type Controller struct {
	mu       sync.Mutex
	running  bool
	registry *registry.Registry
	runner   *runner.Runner
	policy   *policy.Engine
	store    *store.FSStore
	logger   *slog.Logger
}

// NewController creates an UpdateController.
func NewController(
	reg *registry.Registry,
	r *runner.Runner,
	pol *policy.Engine,
	st *store.FSStore,
	logger *slog.Logger,
) *Controller {
	return &Controller{
		registry: reg,
		runner:   r,
		policy:   pol,
		store:    st,
		logger:   logger,
	}
}

// Execute runs the full update state machine.
func (uc *Controller) Execute(ctx context.Context, cfg UpdateConfig) (*UpdateResult, error) {
	runID := uuid.New().String()
	logger := uc.logger.With("run_id", runID, "component", "update")

	result := &UpdateResult{
		RunID:  runID,
		Status: "running",
	}
	start := time.Now()

	// LOCK_ACQUIRE
	if err := uc.tryLock(); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}
	defer uc.unlock()

	// Signal policy engine to block tool executions
	uc.policy.SetUpdateLock(true)
	defer uc.policy.SetUpdateLock(false)

	logger.Info("phase_start", "phase", "lock_acquire")

	// PRE_SNAPSHOT
	prePhase := uc.runPhase("pre_snapshot", func() error {
		return uc.registry.Refresh(ctx)
	})
	result.Phases = append(result.Phases, prePhase)
	if prePhase.Status == "error" {
		result.Status = "error"
		result.Errors = append(result.Errors, "pre-snapshot failed: "+prePhase.Message)
		result.Timing = timingInfo(start)
		return result, fmt.Errorf("pre-snapshot failed")
	}
	preManifest := uc.registry.GetManifest()

	if cfg.DryRun {
		// Skip all update phases, just return current state
		result.Phases = append(result.Phases, PhaseResult{Name: "update_pdtm", Status: "skipped"})
		result.Phases = append(result.Phases, PhaseResult{Name: "update_tools", Status: "skipped"})
		result.Phases = append(result.Phases, PhaseResult{Name: "update_templates", Status: "skipped"})

		postManifest := preManifest
		result.Diff = schema.Diff(preManifest, postManifest)
		result.Status = "success"
		result.Timing = timingInfo(start)
		return result, nil
	}

	// UPDATE_PDTM
	if cfg.IncludePdtm {
		pdtmPhase := uc.runPhase("update_pdtm", func() error {
			return uc.updatePdtm(ctx, runID)
		})
		result.Phases = append(result.Phases, pdtmPhase)
		if pdtmPhase.Status == "error" {
			result.Warnings = append(result.Warnings, "pdtm self-update failed (non-fatal): "+pdtmPhase.Message)
		}
	} else {
		result.Phases = append(result.Phases, PhaseResult{Name: "update_pdtm", Status: "skipped"})
	}

	// UPDATE_TOOLS
	toolsPhase := uc.runPhase("update_tools", func() error {
		return uc.updateTools(ctx, runID, cfg.Tools)
	})
	result.Phases = append(result.Phases, toolsPhase)
	if toolsPhase.Status == "error" {
		result.Warnings = append(result.Warnings, "tool update had errors: "+toolsPhase.Message)
	}

	// UPDATE_TEMPLATES
	if cfg.IncludeTemplates {
		templatesPhase := uc.runPhase("update_templates", func() error {
			return uc.updateTemplates(ctx, runID)
		})
		result.Phases = append(result.Phases, templatesPhase)
		if templatesPhase.Status == "error" {
			result.Warnings = append(result.Warnings, "template update failed (non-fatal): "+templatesPhase.Message)
		}
	} else {
		result.Phases = append(result.Phases, PhaseResult{Name: "update_templates", Status: "skipped"})
	}

	// POST_SNAPSHOT
	postPhase := uc.runPhase("post_snapshot", func() error {
		return uc.registry.Refresh(ctx)
	})
	result.Phases = append(result.Phases, postPhase)
	if postPhase.Status == "error" {
		result.Status = "error"
		result.Errors = append(result.Errors, "post-snapshot failed: "+postPhase.Message)
		result.Timing = timingInfo(start)
		return result, fmt.Errorf("post-snapshot failed")
	}
	postManifest := uc.registry.GetManifest()

	// DIFF_COMPUTE
	result.Diff = schema.Diff(preManifest, postManifest)

	// REGISTRY_REFRESH (already done in POST_SNAPSHOT)
	// Update runner's binary allowlist
	uc.runner.UpdateAllowedBinaries(uc.registry.GetAllowedBinaries())

	// PERSIST_LOG
	_ = uc.persistUpdateArtifacts(runID, preManifest, postManifest, result)

	// Determine final status
	if len(result.Errors) > 0 {
		result.Status = "partial"
	} else {
		result.Status = "success"
	}
	result.Timing = timingInfo(start)

	logger.Info("update_complete",
		"status", result.Status,
		"duration_ms", result.Timing.DurationMs,
	)

	return result, nil
}

func (uc *Controller) tryLock() error {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.running {
		return fmt.Errorf("update already in progress")
	}
	uc.running = true
	return nil
}

func (uc *Controller) unlock() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.running = false
}

func (uc *Controller) runPhase(name string, fn func() error) PhaseResult {
	uc.logger.Info("phase_start", "component", "update", "phase", name)
	start := time.Now()
	err := fn()
	dur := time.Since(start)

	pr := PhaseResult{
		Name:       name,
		DurationMs: dur.Milliseconds(),
	}

	if err != nil {
		pr.Status = "error"
		pr.Message = err.Error()
		uc.logger.Warn("phase_error", "component", "update", "phase", name, "error", err, "duration_ms", dur.Milliseconds())
	} else {
		pr.Status = "success"
		uc.logger.Info("phase_complete", "component", "update", "phase", name, "duration_ms", dur.Milliseconds())
	}

	return pr
}

// InstallAllTools runs pdtm -install-all to install all PD tools.
func (uc *Controller) InstallAllTools(ctx context.Context, runID string) error {
	_, err := uc.runner.Execute(ctx, runner.RunConfig{
		BinaryName: "pdtm",
		Args:       []string{"-install-all", "-duc", "-nc"},
		Timeout:    5 * time.Minute,
		RunID:      runID + "-install-all",
	})
	return err
}

// UpdateTemplates runs nuclei -update-templates to fetch latest templates.
func (uc *Controller) UpdateTemplates(ctx context.Context, runID string) error {
	return uc.updateTemplates(ctx, runID)
}

func (uc *Controller) updatePdtm(ctx context.Context, runID string) error {
	_, err := uc.runner.Execute(ctx, runner.RunConfig{
		BinaryName: "pdtm",
		Args:       []string{"-self-update", "-duc", "-nc"},
		Timeout:    2 * time.Minute,
		RunID:      runID + "-pdtm",
	})
	return err
}

func (uc *Controller) updateTools(ctx context.Context, runID string, tools []string) error {
	args := []string{"-duc", "-nc"}
	if len(tools) == 0 {
		args = append(args, "-update-all")
	} else {
		for _, t := range tools {
			args = append(args, "-install", t)
		}
	}

	_, err := uc.runner.Execute(ctx, runner.RunConfig{
		BinaryName: "pdtm",
		Args:       args,
		Timeout:    5 * time.Minute,
		RunID:      runID + "-tools",
	})
	return err
}

func (uc *Controller) updateTemplates(ctx context.Context, runID string) error {
	_, err := uc.runner.Execute(ctx, runner.RunConfig{
		BinaryName: "nuclei",
		Args:       []string{"-update-templates", "-duc", "-nc"},
		Timeout:    3 * time.Minute,
		RunID:      runID + "-templates",
	})
	return err
}

func (uc *Controller) persistUpdateArtifacts(runID string, pre, post *schema.Manifest, result *UpdateResult) error {
	if uc.store == nil {
		return nil
	}

	ts := time.Now().Format("20060102-150405")
	dir := fmt.Sprintf("%s-%s", ts, runID)

	_ = uc.store.SaveJSON(
		fmt.Sprintf("%s/updates/%s/pre_manifest.json", uc.store.BaseDir(), dir), pre)
	_ = uc.store.SaveJSON(
		fmt.Sprintf("%s/updates/%s/post_manifest.json", uc.store.BaseDir(), dir), post)
	_ = uc.store.SaveJSON(
		fmt.Sprintf("%s/updates/%s/diff.json", uc.store.BaseDir(), dir), result.Diff)

	logData, _ := json.MarshalIndent(result, "", "  ")
	return uc.store.SaveJSON(
		fmt.Sprintf("%s/updates/%s/update.log", uc.store.BaseDir(), dir), json.RawMessage(logData))
}

func timingInfo(start time.Time) schema.TimingInfo {
	end := time.Now()
	return schema.TimingInfo{
		Start:      start.Format(time.RFC3339),
		End:        end.Format(time.RFC3339),
		DurationMs: end.Sub(start).Milliseconds(),
	}
}
