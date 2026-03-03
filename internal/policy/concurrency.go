package policy

import (
	"sync"
)

// ConcurrencyConfig holds per-tool concurrency limits.
type ConcurrencyConfig struct {
	Defaults  ConcurrencyEntry            `yaml:"defaults"`
	Overrides map[string]ConcurrencyEntry `yaml:"overrides"`
}

// ConcurrencyEntry defines max concurrent executions.
type ConcurrencyEntry struct {
	MaxConcurrent int `yaml:"max_concurrent"`
}

// concurrencyLimiter tracks and limits per-tool concurrent executions.
type concurrencyLimiter struct {
	mu        sync.Mutex
	active    map[string]int
	defaults  ConcurrencyEntry
	overrides map[string]ConcurrencyEntry
}

func newConcurrencyLimiter(cfg ConcurrencyConfig) *concurrencyLimiter {
	defaults := cfg.Defaults
	if defaults.MaxConcurrent <= 0 {
		defaults.MaxConcurrent = 3
	}
	return &concurrencyLimiter{
		active:    make(map[string]int),
		defaults:  defaults,
		overrides: cfg.Overrides,
	}
}

// Acquire tries to acquire a concurrency slot. Returns a Denial if full.
func (cl *concurrencyLimiter) Acquire(toolName string) *Denial {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	max := cl.defaults.MaxConcurrent
	if override, ok := cl.overrides[toolName]; ok {
		max = override.MaxConcurrent
	}

	if cl.active[toolName] >= max {
		return &Denial{
			Code:    DenyConcurrency,
			Message: "max concurrent executions reached for " + toolName,
		}
	}

	cl.active[toolName]++
	return nil
}

// Release frees a concurrency slot.
func (cl *concurrencyLimiter) Release(toolName string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.active[toolName] > 0 {
		cl.active[toolName]--
	}
}
