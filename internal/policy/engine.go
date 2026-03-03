package policy

import (
	"log/slog"
	"sync"
	"time"
)

// Denial codes.
const (
	DenyNoScope         = "NO_SCOPE"
	DenyScopeViolation  = "SCOPE_VIOLATION"
	DenyRateLimit       = "RATE_LIMIT"
	DenyConcurrency     = "CONCURRENCY_LIMIT"
	DenyBlockedFlag     = "BLOCKED_FLAG"
	DenyBinaryNotAllowed = "BINARY_NOT_ALLOWED"
	DenyUpdateInProgress = "UPDATE_IN_PROGRESS"
	DenyTimeoutExceeded = "TIMEOUT_EXCEEDED"
)

// Denial represents a policy rejection.
type Denial struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (d *Denial) Error() string {
	return d.Code + ": " + d.Message
}

// EvalRequest is submitted to the Engine for policy evaluation.
type EvalRequest struct {
	ToolName   string
	BinaryName string
	Targets    []string
	Args       []string
	Timeout    time.Duration
}

// EvalResult contains the outcome of a policy evaluation.
type EvalResult struct {
	Allowed        bool
	Denials        []Denial
	ClampedTimeout time.Duration
}

// PolicyConfig holds all policy sub-configurations.
type PolicyConfig struct {
	Scope        ScopeConfig       `yaml:"scope"`
	RateLimits   RateLimitConfig   `yaml:"rate_limits"`
	Concurrency  ConcurrencyConfig `yaml:"concurrency"`
	Timeouts     TimeoutConfig     `yaml:"timeouts"`
	ToolRateCaps ToolRateCapConfig `yaml:"tool_rate_caps"`
	Naabu        NaabuPolicyConfig `yaml:"naabu"`
	Nuclei       NucleiPolicyConfig `yaml:"nuclei"`
}

// NaabuPolicyConfig holds naabu-specific policy.
type NaabuPolicyConfig struct {
	AllowSynScan bool `yaml:"allow_syn_scan"`
}

// NucleiPolicyConfig holds nuclei-specific policy.
type NucleiPolicyConfig struct {
	AllowInteractsh bool `yaml:"allow_interactsh"`
}

// TimeoutConfig holds per-tool timeout bounds.
type TimeoutConfig struct {
	Defaults  TimeoutEntry            `yaml:"defaults"`
	Overrides map[string]TimeoutEntry `yaml:"overrides"`
}

// TimeoutEntry defines default and maximum timeout.
type TimeoutEntry struct {
	Default string `yaml:"default"`
	Max     string `yaml:"max"`
}

// Engine is the central policy evaluation engine.
type Engine struct {
	scope       *scopeChecker
	rateLimit   *rateLimiter
	concurrency *concurrencyLimiter
	timeouts    TimeoutConfig
	naabu       NaabuPolicyConfig
	nuclei      NucleiPolicyConfig
	logger      *slog.Logger

	updateMu  sync.RWMutex
	updating  bool
}

func New(cfg PolicyConfig, logger *slog.Logger) *Engine {
	return &Engine{
		scope:       newScopeChecker(cfg.Scope),
		rateLimit:   newRateLimiter(cfg.RateLimits),
		concurrency: newConcurrencyLimiter(cfg.Concurrency),
		timeouts:    cfg.Timeouts,
		naabu:       cfg.Naabu,
		nuclei:      cfg.Nuclei,
		logger:      logger,
	}
}

// Evaluate runs the full policy evaluation chain.
// Order: update lock → scope → rate limit → concurrency → flags → timeout.
func (e *Engine) Evaluate(req EvalRequest) EvalResult {
	result := EvalResult{Allowed: true}

	// 1. Update lock check
	e.updateMu.RLock()
	updating := e.updating
	e.updateMu.RUnlock()
	if updating {
		result.Allowed = false
		result.Denials = append(result.Denials, Denial{
			Code:    DenyUpdateInProgress,
			Message: "tool execution blocked during ecosystem update",
		})
		e.logger.Warn("eval_deny",
			"component", "policy",
			"tool", req.ToolName,
			"code", DenyUpdateInProgress,
		)
		return result
	}

	// 2. Scope check
	for _, target := range req.Targets {
		if denial := e.scope.CheckTarget(target); denial != nil {
			result.Allowed = false
			result.Denials = append(result.Denials, *denial)
			e.logger.Warn("eval_deny",
				"component", "policy",
				"tool", req.ToolName,
				"code", denial.Code,
				"detail", denial.Detail,
			)
		}
	}
	if !result.Allowed {
		return result
	}

	// 3. Rate limit
	if denial := e.rateLimit.Allow(req.ToolName); denial != nil {
		result.Allowed = false
		result.Denials = append(result.Denials, *denial)
		e.logger.Warn("eval_deny",
			"component", "policy",
			"tool", req.ToolName,
			"code", denial.Code,
		)
		return result
	}

	// 4. Concurrency (acquire slot — caller MUST call ReleaseConcurrency on completion)
	if denial := e.concurrency.Acquire(req.ToolName); denial != nil {
		result.Allowed = false
		result.Denials = append(result.Denials, *denial)
		e.logger.Warn("eval_deny",
			"component", "policy",
			"tool", req.ToolName,
			"code", denial.Code,
		)
		return result
	}

	// 5. Flag blocklist
	if denial := CheckFlags(req.ToolName, req.Args); denial != nil {
		result.Allowed = false
		result.Denials = append(result.Denials, *denial)
		e.concurrency.Release(req.ToolName)
		e.logger.Warn("eval_deny",
			"component", "policy",
			"tool", req.ToolName,
			"code", denial.Code,
			"detail", denial.Detail,
		)
		return result
	}

	// 6. Tool-specific policy
	if denial := e.checkToolSpecific(req); denial != nil {
		result.Allowed = false
		result.Denials = append(result.Denials, *denial)
		e.concurrency.Release(req.ToolName)
		return result
	}

	// 7. Timeout bounds
	result.ClampedTimeout = e.clampTimeout(req.ToolName, req.Timeout)

	e.logger.Info("eval_pass",
		"component", "policy",
		"tool", req.ToolName,
		"targets", req.Targets,
	)

	return result
}

// ReleaseConcurrency frees the concurrency slot after tool execution completes.
func (e *Engine) ReleaseConcurrency(toolName string) {
	e.concurrency.Release(toolName)
}

// SetUpdateLock blocks all tool executions during an update cycle.
func (e *Engine) SetUpdateLock(locked bool) {
	e.updateMu.Lock()
	defer e.updateMu.Unlock()
	e.updating = locked
}

// IsScopeConfigured returns true if any scope allowlists are configured.
func (e *Engine) IsScopeConfigured() bool { return !e.scope.IsEmpty() }

// IsUpdateLocked returns whether the update lock is held.
func (e *Engine) IsUpdateLocked() bool {
	e.updateMu.RLock()
	defer e.updateMu.RUnlock()
	return e.updating
}

func (e *Engine) checkToolSpecific(req EvalRequest) *Denial {
	switch req.ToolName {
	case "naabu":
		if !e.naabu.AllowSynScan {
			for i, arg := range req.Args {
				if arg == "-s" || arg == "--scan-type" {
					// Check the value following the flag — only block SYN scans
					if i+1 < len(req.Args) && req.Args[i+1] == "connect" {
						continue
					}
					return &Denial{
						Code:    DenyBlockedFlag,
						Message: "SYN scan requires explicit opt-in (policy.naabu.allow_syn_scan)",
					}
				}
			}
		}
	case "nuclei":
		if !e.nuclei.AllowInteractsh {
			for _, arg := range req.Args {
				if arg == "-iserver" || arg == "-interactsh-server" {
					return &Denial{
						Code:    DenyBlockedFlag,
						Message: "interactsh requires explicit opt-in (policy.nuclei.allow_interactsh)",
					}
				}
			}
		}
	}
	return nil
}

func (e *Engine) clampTimeout(toolName string, requested time.Duration) time.Duration {
	entry := e.timeouts.Defaults
	if override, ok := e.timeouts.Overrides[toolName]; ok {
		entry = override
	}

	maxDur := parseDuration(entry.Max, 30*time.Minute)
	defaultDur := parseDuration(entry.Default, 5*time.Minute)

	if requested <= 0 {
		return defaultDur
	}
	if requested > maxDur {
		e.logger.Warn("timeout_clamped",
			"component", "policy",
			"tool", toolName,
			"requested", requested.String(),
			"clamped_to", maxDur.String(),
		)
		return maxDur
	}
	return requested
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
