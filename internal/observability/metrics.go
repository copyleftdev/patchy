package observability

import (
	"sync"
	"time"
)

// Metrics tracks in-process counters for tool executions, policy, and updates.
type Metrics struct {
	mu sync.Mutex

	ToolInvocations   map[string]int64
	ToolSuccesses     map[string]int64
	ToolFailures      map[string]int64
	ToolTimeouts      map[string]int64
	ToolDurationSum   map[string]int64
	ToolDurationCount map[string]int64

	PolicyDenials map[string]int64
	RateLimitHits map[string]int64

	UpdatesTotal   int64
	UpdatesSuccess int64
	UpdatesFailure int64

	StartTime time.Time
}

// NewMetrics creates a fresh Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		ToolInvocations:   make(map[string]int64),
		ToolSuccesses:     make(map[string]int64),
		ToolFailures:      make(map[string]int64),
		ToolTimeouts:      make(map[string]int64),
		ToolDurationSum:   make(map[string]int64),
		ToolDurationCount: make(map[string]int64),
		PolicyDenials:     make(map[string]int64),
		RateLimitHits:     make(map[string]int64),
		StartTime:         time.Now(),
	}
}

// RecordInvocation records a tool invocation outcome.
func (m *Metrics) RecordInvocation(tool string, status string, durationMs int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ToolInvocations[tool]++
	m.ToolDurationSum[tool] += durationMs
	m.ToolDurationCount[tool]++

	switch status {
	case "success":
		m.ToolSuccesses[tool]++
	case "timeout":
		m.ToolTimeouts[tool]++
	default:
		m.ToolFailures[tool]++
	}
}

// RecordPolicyDenial records a policy denial by code.
func (m *Metrics) RecordPolicyDenial(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PolicyDenials[code]++
}

// RecordRateLimitHit records a rate limit hit for a tool.
func (m *Metrics) RecordRateLimitHit(tool string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RateLimitHits[tool]++
}

// RecordUpdate records an update attempt.
func (m *Metrics) RecordUpdate(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatesTotal++
	if success {
		m.UpdatesSuccess++
	} else {
		m.UpdatesFailure++
	}
}

// MetricsSnapshot is a point-in-time copy of all metrics.
type MetricsSnapshot struct {
	UptimeSeconds int64                    `json:"uptime_seconds"`
	Tools         map[string]ToolMetrics   `json:"tools"`
	PolicyDenials map[string]int64         `json:"policy_denials"`
	Updates       UpdateMetrics            `json:"updates"`
}

// ToolMetrics holds per-tool counters.
type ToolMetrics struct {
	Invocations   int64 `json:"invocations"`
	Successes     int64 `json:"successes"`
	Failures      int64 `json:"failures"`
	Timeouts      int64 `json:"timeouts"`
	AvgDurationMs int64 `json:"avg_duration_ms"`
}

// UpdateMetrics holds update counters.
type UpdateMetrics struct {
	Total   int64 `json:"total"`
	Success int64 `json:"success"`
	Failure int64 `json:"failure"`
}

// Snapshot returns a point-in-time copy of all metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := MetricsSnapshot{
		UptimeSeconds: int64(time.Since(m.StartTime).Seconds()),
		Tools:         make(map[string]ToolMetrics),
		PolicyDenials: make(map[string]int64),
		Updates: UpdateMetrics{
			Total:   m.UpdatesTotal,
			Success: m.UpdatesSuccess,
			Failure: m.UpdatesFailure,
		},
	}

	for code, count := range m.PolicyDenials {
		snap.PolicyDenials[code] = count
	}

	// Collect all tool names
	toolNames := make(map[string]bool)
	for t := range m.ToolInvocations {
		toolNames[t] = true
	}

	for t := range toolNames {
		tm := ToolMetrics{
			Invocations: m.ToolInvocations[t],
			Successes:   m.ToolSuccesses[t],
			Failures:    m.ToolFailures[t],
			Timeouts:    m.ToolTimeouts[t],
		}
		if m.ToolDurationCount[t] > 0 {
			tm.AvgDurationMs = m.ToolDurationSum[t] / m.ToolDurationCount[t]
		}
		snap.Tools[t] = tm
	}

	return snap
}
