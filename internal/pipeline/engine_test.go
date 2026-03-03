package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestEngine_SimplePipeline(t *testing.T) {
	executor := func(ctx context.Context, toolName string, targets []string, args []string) ([]json.RawMessage, error) {
		var records []json.RawMessage
		for _, tgt := range targets {
			rec, _ := json.Marshal(map[string]string{"host": tgt, "tool": toolName})
			records = append(records, rec)
		}
		return records, nil
	}

	eng := NewEngine(executor, nil, testLogger())

	p := Pipeline{
		Name: "test_pipeline",
		Steps: []Step{
			{
				Name:     "step1",
				ToolName: "tool_a",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-test"}, nil
				},
				Transform: func(records []json.RawMessage) ([]string, error) {
					var hosts []string
					for _, rec := range records {
						var m map[string]string
						json.Unmarshal(rec, &m)
						hosts = append(hosts, m["host"]+".resolved")
					}
					return hosts, nil
				},
			},
			{
				Name:     "step2",
				ToolName: "tool_b",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
		},
	}

	result, err := eng.Execute(context.Background(), p, []string{"a.com", "b.com"}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("expected success, got %s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.Steps[0].Count != 2 {
		t.Fatalf("step1: expected 2 records, got %d", result.Steps[0].Count)
	}
	if result.Steps[1].Count != 2 {
		t.Fatalf("step2: expected 2 records, got %d", result.Steps[1].Count)
	}
}

func TestEngine_EmptyTargets(t *testing.T) {
	called := false
	executor := func(ctx context.Context, toolName string, targets []string, args []string) ([]json.RawMessage, error) {
		called = true
		return nil, nil
	}

	eng := NewEngine(executor, nil, testLogger())

	p := Pipeline{
		Name: "test_empty",
		Steps: []Step{
			{
				Name:     "step1",
				ToolName: "tool_a",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
				Transform: func(records []json.RawMessage) ([]string, error) {
					return nil, nil // empty
				},
			},
			{
				Name:     "step2",
				ToolName: "tool_b",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
		},
	}

	result, _ := eng.Execute(context.Background(), p, []string{"a.com"}, nil)
	if result.Steps[1].Status != "empty" {
		t.Fatalf("expected step2 empty, got %s", result.Steps[1].Status)
	}
	_ = called
}

func TestEngine_StepError_NonOptional(t *testing.T) {
	executor := func(ctx context.Context, toolName string, targets []string, args []string) ([]json.RawMessage, error) {
		if toolName == "fail_tool" {
			return nil, fmt.Errorf("tool crashed")
		}
		return []json.RawMessage{json.RawMessage(`{"ok":true}`)}, nil
	}

	eng := NewEngine(executor, nil, testLogger())

	p := Pipeline{
		Name: "test_error",
		Steps: []Step{
			{
				Name:     "step1",
				ToolName: "fail_tool",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
			{
				Name:     "step2",
				ToolName: "ok_tool",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
		},
	}

	result, err := eng.Execute(context.Background(), p, []string{"a.com"}, nil)
	if err == nil {
		t.Fatal("expected error from failed step")
	}
	if result.Status != "error" {
		t.Fatalf("expected error status, got %s", result.Status)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
}

func TestEngine_StepError_Optional(t *testing.T) {
	executor := func(ctx context.Context, toolName string, targets []string, args []string) ([]json.RawMessage, error) {
		if toolName == "fail_tool" {
			return nil, fmt.Errorf("tool crashed")
		}
		return []json.RawMessage{json.RawMessage(`{"ok":true}`)}, nil
	}

	eng := NewEngine(executor, nil, testLogger())

	p := Pipeline{
		Name: "test_optional",
		Steps: []Step{
			{
				Name:     "step1",
				ToolName: "fail_tool",
				Optional: true,
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
			{
				Name:     "step2",
				ToolName: "ok_tool",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
		},
	}

	result, err := eng.Execute(context.Background(), p, []string{"a.com"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "partial" {
		t.Fatalf("expected partial, got %s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(result.Steps))
	}
}

func TestEngine_Cancellation(t *testing.T) {
	executor := func(ctx context.Context, toolName string, targets []string, args []string) ([]json.RawMessage, error) {
		return []json.RawMessage{json.RawMessage(`{"ok":true}`)}, nil
	}

	eng := NewEngine(executor, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := Pipeline{
		Name: "test_cancel",
		Steps: []Step{
			{
				Name:     "step1",
				ToolName: "tool_a",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, nil, nil
				},
			},
		},
	}

	result, err := eng.Execute(ctx, p, []string{"a.com"}, nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if result.Status != "error" {
		t.Fatalf("expected error status, got %s", result.Status)
	}
}
