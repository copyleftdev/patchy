package policy

import (
	"sync"
	"time"
)

// RateLimitConfig holds per-tool rate limit settings.
type RateLimitConfig struct {
	Defaults  RateLimitEntry            `yaml:"defaults"`
	Overrides map[string]RateLimitEntry `yaml:"overrides"`
}

// RateLimitEntry defines a token bucket rate limit.
type RateLimitEntry struct {
	RequestsPerMin int `yaml:"requests_per_min"`
	Burst          int `yaml:"burst"`
}

// rateLimiter implements a simple per-tool token bucket.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	defaults RateLimitEntry
	overrides map[string]RateLimitEntry
}

type tokenBucket struct {
	tokens    float64
	maxTokens float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newRateLimiter(cfg RateLimitConfig) *rateLimiter {
	defaults := cfg.Defaults
	if defaults.RequestsPerMin <= 0 {
		defaults.RequestsPerMin = 30
	}
	if defaults.Burst <= 0 {
		defaults.Burst = 5
	}
	return &rateLimiter{
		buckets:   make(map[string]*tokenBucket),
		defaults:  defaults,
		overrides: cfg.Overrides,
	}
}

// Allow checks if a tool invocation is allowed under rate limits.
func (rl *rateLimiter) Allow(toolName string) *Denial {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.buckets[toolName]
	if !ok {
		entry := rl.defaults
		if override, exists := rl.overrides[toolName]; exists {
			entry = override
		}
		bucket = &tokenBucket{
			tokens:     float64(entry.Burst),
			maxTokens:  float64(entry.Burst),
			refillRate: float64(entry.RequestsPerMin) / 60.0,
			lastRefill: time.Now(),
		}
		rl.buckets[toolName] = bucket
	}

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * bucket.refillRate
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
	bucket.lastRefill = now

	// Check
	if bucket.tokens < 1.0 {
		return &Denial{
			Code:    DenyRateLimit,
			Message: "rate limit exceeded for " + toolName,
		}
	}

	bucket.tokens -= 1.0
	return nil
}
