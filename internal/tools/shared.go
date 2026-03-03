package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/patchy-mcp/patchy/internal/mcpbridge"
	"github.com/patchy-mcp/patchy/internal/policy"
	"github.com/patchy-mcp/patchy/internal/registry"
	"github.com/patchy-mcp/patchy/internal/runner"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

const patchyVersion = "0.1.0"

// alwaysInjected are flags added to every PD tool invocation.
// Note: JSON output flag is NOT included here because it varies per tool
// (-json for subfinder/dnsx/httpx/naabu, -jsonl for katana/nuclei).
var alwaysInjected = []string{"-duc", "-nc"}

// Deps bundles shared dependencies for all tool handlers.
type Deps struct {
	Runner   *runner.Runner
	Policy   *policy.Engine
	Registry *registry.Registry
	Logger   *slog.Logger
	BaseDir  string
}

// executeTool is the shared handler pattern:
// validate → extract targets → policy check → build args → run → package result.
func executeTool(
	ctx context.Context,
	deps Deps,
	toolName string,
	binaryName string,
	targets []string,
	userArgs []string,
	extraArgs []string,
	recordType string,
	outputFile string,
) (*mcp.CallToolResult, error) {

	runID := uuid.New().String()
	logger := deps.Logger.With("run_id", runID, "tool", toolName)

	// 1. Policy evaluation
	allArgs := make([]string, 0, len(userArgs)+len(extraArgs)+len(alwaysInjected))
	allArgs = append(allArgs, userArgs...)
	allArgs = append(allArgs, extraArgs...)
	allArgs = append(allArgs, alwaysInjected...)

	evalResult := deps.Policy.Evaluate(policy.EvalRequest{
		ToolName:   binaryName,
		BinaryName: binaryName,
		Targets:    targets,
		Args:       allArgs,
	})

	if !evalResult.Allowed {
		logger.Warn("policy_denied", "denials", evalResult.Denials)
		denial := evalResult.Denials[0]
		rr := &schema.RunResult{
			SchemaVersion: schema.RunResultSchemaVersion,
			RunID:         runID,
			Tool:          toolName,
			Status:        "policy_denied",
			Error: &schema.ErrorInfo{
				Code:    denial.Code,
				Message: denial.Message,
				Hint:    hintForDenial(denial.Code),
			},
		}
		return mcpbridge.NewErrorResult(rr), nil
	}
	defer deps.Policy.ReleaseConcurrency(binaryName)

	// 2. Build final args
	finalArgs := make([]string, 0, len(allArgs))
	finalArgs = append(finalArgs, allArgs...)
	if outputFile != "" {
		finalArgs = append(finalArgs, "-o", outputFile)
	}

	// 3. Execute via Runner
	timeout := evalResult.ClampedTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	// Pipe targets as stdin for tools that read from stdin (e.g., -l -)
	var stdin *strings.Reader
	if len(targets) > 0 {
		stdin = strings.NewReader(strings.Join(targets, "\n") + "\n")
	}

	result, err := deps.Runner.Execute(ctx, runner.RunConfig{
		BinaryName: binaryName,
		Args:       finalArgs,
		Stdin:      stdin,
		Timeout:    timeout,
		OutputFile: outputFile,
		RunID:      runID,
	})

	// 4. Package result
	now := time.Now()
	rr := &schema.RunResult{
		SchemaVersion: schema.RunResultSchemaVersion,
		RunID:         runID,
		Tool:          toolName,
		ToolVersion:   deps.Registry.GetToolVersion(binaryName),
		Status:        "success",
		Timing: schema.TimingInfo{
			Start:      now.Format(time.RFC3339),
			End:        now.Format(time.RFC3339),
			DurationMs: 0,
		},
		Result: schema.ResultPayload{
			RecordType: recordType,
		},
		Environment: schema.EnvironmentInfo{
			PatchyVersion:    patchyVersion,
			ToolVersion:      deps.Registry.GetToolVersion(binaryName),
			TemplatesVersion: deps.Registry.GetTemplatesVersion(),
			OS:               runtime.GOOS,
			Arch:             runtime.GOARCH,
		},
	}

	if result != nil {
		rr.BinaryPath = result.BinaryPath
		rr.Invocation = schema.InvocationInfo{
			Args: result.Args,
			Cwd:  result.Cwd,
		}
		rr.Timing = schema.TimingInfo{
			Start:      result.StartTime.Format(time.RFC3339),
			End:        result.EndTime.Format(time.RFC3339),
			DurationMs: result.Duration.Milliseconds(),
		}
		rr.Result.Records = result.OutputRecords
		rr.Result.Count = len(result.OutputRecords)
		rr.Result.Stdout = result.Stdout.String()
		rr.Result.Stderr = result.Stderr.String()
		rr.Result.Truncated = result.Stdout.Truncated() || result.Stderr.Truncated()
	}

	if err != nil {
		if rErr, ok := err.(*runner.RunnerError); ok {
			rr.Status = mapErrorStatus(rErr.Code)
			rr.Error = &schema.ErrorInfo{
				Code:    rErr.Code,
				Message: rErr.Message,
				Hint:    hintForError(rErr.Code, binaryName),
			}
		} else {
			rr.Status = "error"
			rr.Error = &schema.ErrorInfo{
				Code:    runner.ErrExecutionFailed,
				Message: err.Error(),
				Hint:    hintForError(runner.ErrExecutionFailed, binaryName),
			}
		}
		return mcpbridge.NewErrorResult(rr), nil
	}

	return mcpbridge.NewResult(rr), nil
}

func hintForError(code, binaryName string) string {
	switch code {
	case runner.ErrBinaryNotFound:
		return fmt.Sprintf("Install: pdtm -install %s, or call pd.ecosystem.setup", binaryName)
	case runner.ErrBinaryNotAllowed:
		return fmt.Sprintf("%s is not in the binary allowlist; refresh registry or reinstall", binaryName)
	case runner.ErrExecutionTimeout:
		return "Increase timeout or reduce target scope"
	case runner.ErrSandboxError:
		return fmt.Sprintf("Check binary permissions: ls -la $(which %s)", binaryName)
	default:
		return ""
	}
}

func hintForDenial(code string) string {
	switch code {
	case policy.DenyNoScope:
		return "Set PATCHY_SCOPE=<target> or configure policy.scope in patchy.yaml"
	case policy.DenyScopeViolation:
		return "Add the target to policy.scope.allow_domains or allow_cidrs"
	case policy.DenyRateLimit:
		return "Wait and retry, or increase policy.rate_limits in config"
	case policy.DenyConcurrency:
		return "Wait for running tools to finish, or increase policy.concurrency in config"
	case policy.DenyBlockedFlag:
		return "Remove the blocked flag or enable it in policy config"
	case policy.DenyUpdateInProgress:
		return "Wait for the ecosystem update to complete"
	default:
		return ""
	}
}

func mapErrorStatus(code string) string {
	switch code {
	case runner.ErrExecutionTimeout:
		return "timeout"
	case runner.ErrExecutionCancelled:
		return "cancelled"
	default:
		return "error"
	}
}

// getArgs safely extracts the arguments map from an MCP CallToolRequest.
func getArgs(req mcp.CallToolRequest) map[string]interface{} {
	if req.Params.Arguments == nil {
		return make(map[string]interface{})
	}
	args, ok := req.Params.Arguments.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}
	return args
}

// getString extracts a string param from MCP request args.
func getString(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// getStringArray extracts a string array param from MCP request args.
func getStringArray(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return val
	case string:
		return strings.Split(val, ",")
	}
	return nil
}

// getNumber extracts a numeric param from MCP request args.
func getNumber(args map[string]interface{}, key string) (float64, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}
	return 0, false
}

// getBool extracts a boolean param from MCP request args.
func getBool(args map[string]interface{}, key string) (bool, bool) {
	v, ok := args[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// addFlag appends -flag value to args if value is non-empty.
func addFlag(args *[]string, flag, value string) {
	if value != "" {
		*args = append(*args, flag, value)
	}
}

// addFlagInt appends -flag N to args if N > 0.
func addFlagInt(args *[]string, flag string, value int) {
	if value > 0 {
		*args = append(*args, flag, fmt.Sprintf("%d", value))
	}
}

// addFlagBool appends -flag to args if value is true.
func addFlagBool(args *[]string, flag string, value bool) {
	if value {
		*args = append(*args, flag)
	}
}
