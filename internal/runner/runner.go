package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultMaxStdout  int64         = 10 << 20 // 10 MB
	defaultMaxStderr  int64         = 1 << 20  // 1 MB
	defaultTimeout                  = 5 * time.Minute
	maxRecordsDefault               = 10000
)

// Error codes returned by the Runner.
const (
	ErrBinaryNotAllowed  = "BINARY_NOT_ALLOWED"
	ErrBinaryNotFound    = "BINARY_NOT_FOUND"
	ErrExecutionFailed   = "EXECUTION_FAILED"
	ErrExecutionTimeout  = "EXECUTION_TIMEOUT"
	ErrExecutionCancelled = "EXECUTION_CANCELLED"
	ErrOutputParseError  = "OUTPUT_PARSE_ERROR"
	ErrSandboxError      = "SANDBOX_ERROR"
)

// RunnerError is a structured error from the Runner.
type RunnerError struct {
	Code    string
	Message string
}

func (e *RunnerError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Config holds Runner configuration.
type Config struct {
	AllowedBinaries map[string]string // name → absolute path
	BaseOutputDir   string
	MaxStdout       int64
	MaxStderr       int64
	DefaultTimeout  time.Duration
}

// Runner executes PD tool binaries with sandbox and limits.
type Runner struct {
	mu              sync.RWMutex
	allowedBinaries map[string]string
	baseOutputDir   string
	maxStdout       int64
	maxStderr       int64
	defaultTimeout  time.Duration
	logger          *slog.Logger
}

// RunConfig describes a single binary execution.
type RunConfig struct {
	BinaryName string
	Args       []string
	Stdin      io.Reader
	Timeout    time.Duration
	OutputFile string
	EnvVars    map[string]string
	RunID      string
}

// RunResult captures everything about a completed execution.
type RunResult struct {
	RunID         string
	ExitCode      int
	Stdout        *BoundedBuffer
	Stderr        *BoundedBuffer
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	TimedOut      bool
	Cancelled     bool
	OutputRecords []json.RawMessage
	Skipped       int
	BinaryPath    string
	Args          []string
	Cwd           string
}

func New(cfg Config, logger *slog.Logger) *Runner {
	maxOut := cfg.MaxStdout
	if maxOut <= 0 {
		maxOut = defaultMaxStdout
	}
	maxErr := cfg.MaxStderr
	if maxErr <= 0 {
		maxErr = defaultMaxStderr
	}
	timeout := cfg.DefaultTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Runner{
		allowedBinaries: cfg.AllowedBinaries,
		baseOutputDir:   cfg.BaseOutputDir,
		maxStdout:       maxOut,
		maxStderr:       maxErr,
		defaultTimeout:  timeout,
		logger:          logger,
	}
}

// Execute runs a binary with sandbox and limits. It is safe for concurrent use.
func (r *Runner) Execute(ctx context.Context, rc RunConfig) (*RunResult, error) {
	// 1. Resolve binary path from allowlist
	r.mu.RLock()
	binaryPath, ok := r.allowedBinaries[rc.BinaryName]
	r.mu.RUnlock()
	if !ok {
		return nil, &RunnerError{Code: ErrBinaryNotAllowed, Message: fmt.Sprintf("binary %q not in allowlist", rc.BinaryName)}
	}

	// 2. Validate binary exists + is executable
	info, err := os.Stat(binaryPath)
	if err != nil {
		return nil, &RunnerError{Code: ErrBinaryNotFound, Message: fmt.Sprintf("binary not found at %s: %v", binaryPath, err)}
	}
	if info.Mode()&0111 == 0 {
		return nil, &RunnerError{Code: ErrBinaryNotFound, Message: fmt.Sprintf("binary not executable: %s", binaryPath)}
	}

	// 3. Create sandboxed working directory
	runDir := filepath.Join(r.baseOutputDir, "runs", rc.RunID)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return nil, &RunnerError{Code: ErrSandboxError, Message: fmt.Sprintf("failed to create run dir: %v", err)}
	}

	// 4. Build exec.Cmd
	timeout := rc.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, binaryPath, rc.Args...)
	cmd.Dir = runDir
	cmd.Env = r.buildEnv(rc.EnvVars)

	stdoutBuf := NewBoundedBuffer(r.maxStdout)
	stderrBuf := NewBoundedBuffer(r.maxStderr)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if rc.Stdin != nil {
		cmd.Stdin = rc.Stdin
	}

	// Cancel handling: use process group so we can signal the whole group
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	result := &RunResult{
		RunID:      rc.RunID,
		BinaryPath: binaryPath,
		Args:       rc.Args,
		Cwd:        runDir,
		Stdout:     stdoutBuf,
		Stderr:     stderrBuf,
	}

	r.logger.Info("exec_start",
		"component", "runner",
		"run_id", rc.RunID,
		"tool", rc.BinaryName,
		"binary", binaryPath,
		"args", rc.Args,
	)

	// 6. Start process
	result.StartTime = time.Now()
	if err := cmd.Start(); err != nil {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, &RunnerError{Code: ErrExecutionFailed, Message: fmt.Sprintf("failed to start: %v", err)}
	}

	// 7. Wait for completion
	waitErr := cmd.Wait()
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Determine timeout / cancellation.
	// Note: cmd.Wait() already returned, so the process is reaped.
	// exec.CommandContext sends SIGKILL on context expiry before Wait returns.
	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		r.logger.Warn("exec_timeout",
			"component", "runner",
			"run_id", rc.RunID,
			"tool", rc.BinaryName,
			"duration_ms", result.Duration.Milliseconds(),
		)
		return result, &RunnerError{Code: ErrExecutionTimeout, Message: "execution timed out"}
	}
	if execCtx.Err() == context.Canceled {
		result.Cancelled = true
		return result, &RunnerError{Code: ErrExecutionCancelled, Message: "execution cancelled"}
	}

	// 8. Parse JSONL output
	var parseSource io.Reader
	if rc.OutputFile != "" {
		outputPath := filepath.Join(runDir, rc.OutputFile)
		f, err := os.Open(outputPath)
		if err != nil {
			r.logger.Warn("output_file_missing",
				"component", "runner",
				"run_id", rc.RunID,
				"path", outputPath,
				"error", err,
			)
		} else {
			defer f.Close()
			parseSource = f
		}
	}
	if parseSource == nil {
		parseSource = strings.NewReader(stdoutBuf.String())
	}

	records, skipped, parseErr := ParseJSONL(parseSource, maxRecordsDefault)
	if parseErr != nil {
		r.logger.Warn("jsonl_parse_error",
			"component", "runner",
			"run_id", rc.RunID,
			"error", parseErr,
		)
	}
	result.OutputRecords = records
	result.Skipped = skipped

	r.logger.Info("exec_complete",
		"component", "runner",
		"run_id", rc.RunID,
		"tool", rc.BinaryName,
		"exit_code", result.ExitCode,
		"duration_ms", result.Duration.Milliseconds(),
		"records", len(records),
		"skipped_lines", skipped,
	)

	if waitErr != nil && result.ExitCode != 0 {
		return result, &RunnerError{
			Code:    ErrExecutionFailed,
			Message: fmt.Sprintf("exit code %d: %s", result.ExitCode, stderrBuf.String()),
		}
	}

	return result, nil
}

// GetAllowedBinaries returns a copy of the current allowlist.
func (r *Runner) GetAllowedBinaries() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]string, len(r.allowedBinaries))
	for k, v := range r.allowedBinaries {
		cp[k] = v
	}
	return cp
}

// UpdateAllowedBinaries replaces the binary allowlist (e.g., after registry refresh).
func (r *Runner) UpdateAllowedBinaries(binaries map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowedBinaries = binaries
}

func (r *Runner) buildEnv(extra map[string]string) []string {
	env := []string{
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"PATCHY=1",
		"PATH=" + r.buildRestrictedPath(),
	}
	for k, v := range extra {
		if strings.ToUpper(k) == "PATH" {
			continue // never override PATH
		}
		env = append(env, k+"="+v)
	}
	return env
}

// buildRestrictedPath constructs a PATH from the parent directories of all
// allowed binaries. This lets PD tools find sibling binaries and their own
// config without exposing the full system PATH.
func (r *Runner) buildRestrictedPath() string {
	seen := make(map[string]bool)
	var dirs []string
	r.mu.RLock()
	for _, p := range r.allowedBinaries {
		dir := filepath.Dir(p)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	r.mu.RUnlock()
	return strings.Join(dirs, ":")
}
