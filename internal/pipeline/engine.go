package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/patchy-mcp/patchy/internal/store"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

// Step defines a single pipeline stage.
type Step struct {
	Name       string
	ToolName   string
	BuildArgs  func(ctx context.Context, input StepInput) (targets []string, args []string, err error)
	Transform  func(records []json.RawMessage) ([]string, error) // output records → next step targets
	Optional   bool
}

// StepInput carries data into a step.
type StepInput struct {
	Targets []string
	Records []json.RawMessage
}

// Pipeline defines a multi-step workflow.
type Pipeline struct {
	Name        string
	Description string
	Steps       []Step
}

// StepResult captures the outcome of one pipeline step.
type StepResult struct {
	Name       string             `json:"name"`
	Tool       string             `json:"tool"`
	Status     string             `json:"status"` // success | error | skipped | empty
	Records    []json.RawMessage  `json:"records,omitempty"`
	Count      int                `json:"count"`
	DurationMs int64              `json:"duration_ms"`
	Error      string             `json:"error,omitempty"`
}

// PipelineResult captures the full pipeline execution.
type PipelineResult struct {
	PipelineID string             `json:"pipeline_id"`
	Name       string             `json:"name"`
	Status     string             `json:"status"` // success | partial | error
	Steps      []StepResult       `json:"steps"`
	Timing     schema.TimingInfo  `json:"timing"`
}

// Checkpoint persists pipeline state for resumption.
type Checkpoint struct {
	PipelineID    string           `json:"pipeline_id"`
	Name          string           `json:"name"`
	CompletedStep int              `json:"completed_step"`
	StepResults   []StepResult     `json:"step_results"`
	NextTargets   []string         `json:"next_targets"`
	NextRecords   []json.RawMessage `json:"next_records"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// ToolExecutor is the interface the engine uses to run individual tools.
type ToolExecutor func(ctx context.Context, toolName string, targets []string, args []string) (records []json.RawMessage, err error)

// Engine executes pipelines.
type Engine struct {
	executor ToolExecutor
	store    *store.FSStore
	logger   *slog.Logger
}

// NewEngine creates a pipeline engine.
func NewEngine(executor ToolExecutor, st *store.FSStore, logger *slog.Logger) *Engine {
	return &Engine{
		executor: executor,
		store:    st,
		logger:   logger,
	}
}

// Execute runs a pipeline from start or from a checkpoint.
func (e *Engine) Execute(ctx context.Context, p Pipeline, initialTargets []string, resumeFrom *Checkpoint) (*PipelineResult, error) {
	pipelineID := uuid.New().String()
	logger := e.logger.With("pipeline_id", pipelineID, "pipeline", p.Name, "component", "pipeline")

	result := &PipelineResult{
		PipelineID: pipelineID,
		Name:       p.Name,
		Status:     "running",
	}
	start := time.Now()

	startStep := 0
	currentTargets := initialTargets
	var currentRecords []json.RawMessage

	// Resume from checkpoint if provided
	if resumeFrom != nil {
		pipelineID = resumeFrom.PipelineID
		result.PipelineID = pipelineID
		startStep = resumeFrom.CompletedStep + 1
		result.Steps = resumeFrom.StepResults
		currentTargets = resumeFrom.NextTargets
		currentRecords = resumeFrom.NextRecords
		logger.Info("resuming_from_checkpoint", "step", startStep)
	}

	for i := startStep; i < len(p.Steps); i++ {
		step := p.Steps[i]

		select {
		case <-ctx.Done():
			result.Status = "error"
			e.saveCheckpoint(pipelineID, p.Name, i-1, result.Steps, currentTargets, currentRecords)
			result.Timing = pipelineTiming(start)
			return result, ctx.Err()
		default:
		}

		logger.Info("step_start", "step", step.Name, "tool", step.ToolName, "target_count", len(currentTargets))

		if len(currentTargets) == 0 && i > 0 {
			sr := StepResult{
				Name:   step.Name,
				Tool:   step.ToolName,
				Status: "empty",
			}
			if step.Optional {
				sr.Status = "skipped"
			}
			result.Steps = append(result.Steps, sr)
			logger.Info("step_empty", "step", step.Name)
			continue
		}

		// Build args for this step
		targets, args, err := step.BuildArgs(ctx, StepInput{
			Targets: currentTargets,
			Records: currentRecords,
		})
		if err != nil {
			sr := StepResult{
				Name:   step.Name,
				Tool:   step.ToolName,
				Status: "error",
				Error:  fmt.Sprintf("build args failed: %v", err),
			}
			result.Steps = append(result.Steps, sr)
			if !step.Optional {
				result.Status = "error"
				result.Timing = pipelineTiming(start)
				return result, err
			}
			continue
		}

		// Execute tool
		stepStart := time.Now()
		records, err := e.executor(ctx, step.ToolName, targets, args)
		stepDur := time.Since(stepStart)

		sr := StepResult{
			Name:       step.Name,
			Tool:       step.ToolName,
			Records:    records,
			Count:      len(records),
			DurationMs: stepDur.Milliseconds(),
		}

		if err != nil {
			sr.Status = "error"
			sr.Error = err.Error()
			result.Steps = append(result.Steps, sr)
			logger.Warn("step_error", "step", step.Name, "error", err)

			if !step.Optional {
				result.Status = "error"
				e.saveCheckpoint(pipelineID, p.Name, i-1, result.Steps, currentTargets, currentRecords)
				result.Timing = pipelineTiming(start)
				return result, err
			}
			continue
		}

		sr.Status = "success"
		result.Steps = append(result.Steps, sr)

		// Save step result
		if e.store != nil {
			_ = e.store.SaveStepResult(pipelineID, step.Name, sr)
		}

		logger.Info("step_complete", "step", step.Name, "records", len(records), "duration_ms", stepDur.Milliseconds())

		// Transform records to next step targets
		if step.Transform != nil && i < len(p.Steps)-1 {
			nextTargets, err := step.Transform(records)
			if err != nil {
				logger.Warn("transform_error", "step", step.Name, "error", err)
			} else {
				currentTargets = nextTargets
				currentRecords = records
			}
		} else {
			currentRecords = records
		}

		// Checkpoint after each step
		e.saveCheckpoint(pipelineID, p.Name, i, result.Steps, currentTargets, currentRecords)
	}

	// Determine final status
	hasError := false
	for _, sr := range result.Steps {
		if sr.Status == "error" {
			hasError = true
			break
		}
	}
	if hasError {
		result.Status = "partial"
	} else {
		result.Status = "success"
	}

	result.Timing = pipelineTiming(start)

	// Save final report
	if e.store != nil {
		_ = e.store.SavePipelineResult(pipelineID, result)
	}

	logger.Info("pipeline_complete",
		"status", result.Status,
		"steps", len(result.Steps),
		"duration_ms", result.Timing.DurationMs,
	)

	return result, nil
}

func (e *Engine) saveCheckpoint(pipelineID, name string, completedStep int, steps []StepResult, targets []string, records []json.RawMessage) {
	if e.store == nil {
		return
	}
	cp := Checkpoint{
		PipelineID:    pipelineID,
		Name:          name,
		CompletedStep: completedStep,
		StepResults:   steps,
		NextTargets:   targets,
		NextRecords:   records,
		UpdatedAt:     time.Now(),
	}
	_ = e.store.SaveCheckpoint(pipelineID, cp)
}

func pipelineTiming(start time.Time) schema.TimingInfo {
	end := time.Now()
	return schema.TimingInfo{
		Start:      start.Format(time.RFC3339),
		End:        end.Format(time.RFC3339),
		DurationMs: end.Sub(start).Milliseconds(),
	}
}
